# Mystery Friend Reveal-on-Defense Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reveal the declarer's mystery friend only when they win a trick worth defending (a point card in it, or a joker win), replacing today's "reveal when the called card is played" trigger — a `go-mighty` backend change.

**Architecture:** Add a server-side `friendSeat()` helper that derives the friend's seat from existing state (hands + played tricks), so no new persisted field is needed. The public `PartnerSeat`/`partner_seat` field becomes a reveal signal only, set at trick resolution when the friend wins a qualifying trick. Point scoring switches from `PartnerSeat` to `friendSeat()` so team attribution is correct even when the friend is never revealed.

**Tech Stack:** Go. Unit tests in `internal/game` (`go test`), integration e2e via godog against a Docker Compose stack (`go test -tags=integration`).

## Global Constraints

- Backend only (`go-mighty`). No frontend or JSON/API-shape changes. `partner_seat` keeps its tag; its meaning becomes "revealed seat, or -1 if not yet revealed."
- The backend is the source of truth for rules.
- Point-card set = `Card.IsPointCard()` = {A, K, Q, J, 10}. The mighty is an Ace (already a point card); the **joker** is the only special that needs its own clause.
- Reveal fires only when the friend is the trick winner AND (the trick contains ≥1 point card played by anyone, OR the friend's own winning card is the joker).
- Reveal is monotonic: once `PartnerSeat >= 0`, never unset.
- No scoring *values* change; only the team-attribution source changes to `friendSeat()`.
- `friendSeat()` derives the seat from state (never a new stored field).
- Tests live in `package game` (same package), so they may call the unexported `friendSeat()`.
- Run from `/Users/joekhosbayar/mighty/go-mighty`. Unit tests: `go test ./internal/game/`. Full: `go test ./...`. E2e: `docker compose up -d --build` then `go test -tags=integration ./tests/e2e/...`.

---

### Task 1: `friendSeat()` helper

**Files:**
- Modify: `internal/game/rules.go` (add method near the other helpers, after `IsJokerCaller`, ~line 378)
- Test: `internal/game/rules_friend_test.go` (append)

**Interfaces:**
- Consumes: existing `Game` fields `IsNoFriend`, `PartnerCard`, `Players[].Hand`, `Tricks[].Cards`.
- Produces: `func (g *Game) friendSeat() int` — returns the seat holding or having-played `PartnerCard`, or -1 when there is no friend or the card is unheld. Later tasks call this for reveal detection and scoring.

- [ ] **Step 1: Write the failing tests**

Append to `internal/game/rules_friend_test.go`:

```go
func TestFriendSeatFindsCardInHand(t *testing.T) {
	t.Parallel()

	g := callingGame()
	g.PartnerCard = &Card{Suit: Hearts, Rank: King}
	g.Players[3].Hand = []Card{{Suit: Clubs, Rank: Two}, {Suit: Hearts, Rank: King}}

	if got := g.friendSeat(); got != 3 {
		t.Fatalf("friendSeat() = %d, want 3", got)
	}
}

func TestFriendSeatFindsCardAlreadyPlayed(t *testing.T) {
	t.Parallel()

	g := callingGame()
	g.PartnerCard = &Card{Suit: Hearts, Rank: King}
	g.Tricks = []Trick{{
		Winner:   2,
		LeadSuit: Clubs,
		Cards: []PlayedCard{
			{PlayerID: "p1", Seat: 1, Card: Card{Suit: Clubs, Rank: Nine}},
			{PlayerID: "p2", Seat: 2, Card: Card{Suit: Hearts, Rank: King}},
		},
	}}

	if got := g.friendSeat(); got != 2 {
		t.Fatalf("friendSeat() = %d, want 2", got)
	}
}

func TestFriendSeatNoFriendOrUnheld(t *testing.T) {
	t.Parallel()

	noFriend := callingGame()
	noFriend.IsNoFriend = true
	if got := noFriend.friendSeat(); got != -1 {
		t.Fatalf("no-friend friendSeat() = %d, want -1", got)
	}

	nilCard := callingGame()
	if got := nilCard.friendSeat(); got != -1 {
		t.Fatalf("nil partner card friendSeat() = %d, want -1", got)
	}

	unheld := callingGame()
	unheld.PartnerCard = &Card{Suit: Hearts, Rank: King} // held by nobody, not yet played
	if got := unheld.friendSeat(); got != -1 {
		t.Fatalf("unheld friendSeat() = %d, want -1", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/game/ -run TestFriendSeat`
Expected: FAIL — `g.friendSeat undefined` (compile error).

- [ ] **Step 3: Add the helper**

In `internal/game/rules.go`, immediately after the `IsJokerCaller` method (ends ~line 378), add:

```go
// friendSeat returns the seat of the mystery friend (the holder of the called
// partner card), or -1 when there is no friend or the card is unheld (e.g. the
// declarer discarded it into the kitty before calling). It scans current hands
// and every played trick card, so it is correct at any point after the friend
// is called and needs no stored field — it survives Redis reloads for free.
func (g *Game) friendSeat() int {
	if g.IsNoFriend || g.PartnerCard == nil {
		return -1
	}

	pc := *g.PartnerCard

	for _, p := range g.Players {
		if p == nil {
			continue
		}

		for _, c := range p.Hand {
			if c.Suit == pc.Suit && c.Rank == pc.Rank {
				return p.Seat
			}
		}
	}

	for _, t := range g.Tricks {
		for _, played := range t.Cards {
			if played.Card.Suit == pc.Suit && played.Card.Rank == pc.Rank {
				return played.Seat
			}
		}
	}

	return -1
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/game/ -run TestFriendSeat`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/rules.go internal/game/rules_friend_test.go
git commit -m "feat(game): add friendSeat helper deriving the mystery friend from state"
```

---

### Task 2: Reveal on defense (replace the called-card reveal)

**Files:**
- Modify: `internal/game/rules.go` — remove the old reveal (~lines 593-596), add reveal in the trick-resolution block (~line 616, after `g.Tricks[idx].Winner = winnerSeat`), add `trickRevealsFriend` helper
- Test: `internal/game/rules_friend_test.go` — add reveal tests; rewrite the two called-card-reveal tests

**Interfaces:**
- Consumes: `g.friendSeat()` (Task 1); `Card.IsPointCard()`; `Trick`, `PlayedCard`, `Card`, rank/suit constants.
- Produces: `func trickRevealsFriend(t Trick, friendSeat int) bool`. Reveal side effect: sets `g.PartnerSeat` at trick resolution.

- [ ] **Step 1: Write the new reveal tests and rewrite the two stale ones**

In `internal/game/rules_friend_test.go`, add this shared fixture and the new tests:

```go
// revealFixture builds a hearts-trump playing-phase game (declarer seat 0) on
// its second trick, so the joker keeps power. `down` are the four cards already
// played (Seat fields set), `lead` is the led suit, and `turn` is the seat about
// to play the deciding fifth card. Callers set PartnerCard and the relevant hand.
func revealFixture(turn int, down []PlayedCard, lead Suit) *Game {
	g := callingGame()
	g.Trump = Hearts
	g.Status = PhasePlaying
	g.CurrentTurn = turn
	g.Tricks = []Trick{
		{Winner: 0, Cards: []PlayedCard{}},
		{LeadSuit: lead, Cards: down},
	}

	return g
}

func lowClubs() []PlayedCard {
	return []PlayedCard{
		{PlayerID: "p0", Seat: 0, Card: Card{Suit: Clubs, Rank: Two}},
		{PlayerID: "p1", Seat: 1, Card: Card{Suit: Clubs, Rank: Three}},
		{PlayerID: "p3", Seat: 3, Card: Card{Suit: Clubs, Rank: Four}},
		{PlayerID: "p4", Seat: 4, Card: Card{Suit: Clubs, Rank: Five}},
	}
}

func TestFriendRevealedWinningWithPointCard(t *testing.T) {
	t.Parallel()

	g := revealFixture(2, lowClubs(), Clubs)
	g.PartnerCard = &Card{Suit: Clubs, Rank: King}
	g.Players[2].Hand = []Card{{Suit: Clubs, Rank: King}}

	if err := g.ApplyMove("p2", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: King}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != 2 {
		t.Fatalf("expected reveal to seat 2, got %d", g.PartnerSeat)
	}
}

func TestFriendRevealedDefendingOpponentPointCard(t *testing.T) {
	t.Parallel()

	down := []PlayedCard{
		{PlayerID: "p0", Seat: 0, Card: Card{Suit: Clubs, Rank: King}}, // opponent's point card
		{PlayerID: "p1", Seat: 1, Card: Card{Suit: Clubs, Rank: Two}},
		{PlayerID: "p3", Seat: 3, Card: Card{Suit: Clubs, Rank: Three}},
		{PlayerID: "p4", Seat: 4, Card: Card{Suit: Clubs, Rank: Four}},
	}
	g := revealFixture(2, down, Clubs)
	g.PartnerCard = &Card{Suit: Diamonds, Rank: Eight} // friend identity, stays in hand
	g.Players[2].Hand = []Card{{Suit: Hearts, Rank: Seven}, {Suit: Diamonds, Rank: Eight}}

	// Seat 2 is void in clubs and wins the trick with a trump (hearts).
	if err := g.ApplyMove("p2", MovePlayCard, PlayCardMove{Card: Card{Suit: Hearts, Rank: Seven}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != 2 {
		t.Fatalf("expected reveal to seat 2 (defended ♣K), got %d", g.PartnerSeat)
	}
}

func TestFriendRevealedWinningWithJoker(t *testing.T) {
	t.Parallel()

	g := revealFixture(2, lowClubs(), Clubs)
	g.PartnerCard = &Card{Suit: Diamonds, Rank: Eight}
	g.Players[2].Hand = []Card{{Suit: None, Rank: Joker}, {Suit: Diamonds, Rank: Eight}}

	if err := g.ApplyMove("p2", MovePlayCard, PlayCardMove{Card: Card{Suit: None, Rank: Joker}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != 2 {
		t.Fatalf("expected reveal to seat 2 via joker, got %d", g.PartnerSeat)
	}
}

func TestFriendNotRevealedWinningPointlessTrick(t *testing.T) {
	t.Parallel()

	g := revealFixture(2, lowClubs(), Clubs)
	g.PartnerCard = &Card{Suit: Clubs, Rank: King}
	g.Players[2].Hand = []Card{{Suit: Clubs, Rank: Nine}, {Suit: Clubs, Rank: King}}

	// Seat 2 wins with ♣9 — no point card, no joker.
	if err := g.ApplyMove("p2", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Nine}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != -1 {
		t.Fatalf("expected no reveal on a pointless win, got %d", g.PartnerSeat)
	}
}

func TestNonFriendWinningPointTrickDoesNotReveal(t *testing.T) {
	t.Parallel()

	down := []PlayedCard{
		{PlayerID: "p0", Seat: 0, Card: Card{Suit: Clubs, Rank: King}},
		{PlayerID: "p1", Seat: 1, Card: Card{Suit: Clubs, Rank: Two}},
		{PlayerID: "p2", Seat: 2, Card: Card{Suit: Clubs, Rank: Six}}, // the friend, already played and losing
		{PlayerID: "p4", Seat: 4, Card: Card{Suit: Clubs, Rank: Four}},
	}
	g := revealFixture(3, down, Clubs)
	g.PartnerCard = &Card{Suit: Clubs, Rank: Six} // friend = seat 2 (played ♣6)
	g.Players[3].Hand = []Card{{Suit: Clubs, Rank: Ace}}

	// Seat 3 (a non-friend) wins with ♣A.
	if err := g.ApplyMove("p3", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Ace}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != -1 {
		t.Fatalf("expected no reveal when a non-friend wins, got %d", g.PartnerSeat)
	}
}

func TestRevealIsMonotonic(t *testing.T) {
	t.Parallel()

	g := revealFixture(2, lowClubs(), Clubs)
	g.PartnerCard = &Card{Suit: Clubs, Rank: King}
	g.PartnerSeat = 2 // already revealed
	g.Players[2].Hand = []Card{{Suit: Clubs, Rank: Nine}, {Suit: Clubs, Rank: King}}

	// A later pointless win must not un-reveal.
	if err := g.ApplyMove("p2", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Nine}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != 2 {
		t.Fatalf("reveal was lost, got %d", g.PartnerSeat)
	}
}
```

Then **replace** the existing `TestPlayingCalledCardRevealsPartner` (the whole function) with:

```go
func TestPlayingCalledCardAloneDoesNotReveal(t *testing.T) {
	t.Parallel()

	g := playingGameWithPartnerCard() // seat 2 leads ♦A into the open trick 2
	move := PlayCardMove{Card: Card{Suit: Diamonds, Rank: Ace}}

	if err := g.ApplyMove("p2", MovePlayCard, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != -1 {
		t.Fatalf("playing the called card must not reveal on its own, got %d", g.PartnerSeat)
	}
}
```

And **replace** the existing `TestDeclarerPlayingOwnCalledCardIsSelfPartner` (the whole function) with:

```go
func TestDeclarerPlayingOwnCalledCardDoesNotRevealAlone(t *testing.T) {
	t.Parallel()

	g := playingGameWithPartnerCard()
	g.CurrentTurn = 0
	g.Players[0].Hand = []Card{{Suit: Diamonds, Rank: Ace}}
	g.Players[2].Hand = []Card{{Suit: Clubs, Rank: Two}}

	move := PlayCardMove{Card: Card{Suit: Diamonds, Rank: Ace}}
	if err := g.ApplyMove("p0", MovePlayCard, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != -1 {
		t.Fatalf("playing own called card must not reveal on its own, got %d", g.PartnerSeat)
	}

	if fs := g.friendSeat(); fs != 0 {
		t.Fatalf("friendSeat() = %d, want 0 (declarer holds/played the called card)", fs)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/game/ -run 'Friend|Reveal|CalledCard'`
Expected: FAIL — new reveal tests fail (no reveal happens yet / `trickRevealsFriend` undefined), and the two rewritten tests fail against the still-present old reveal.

- [ ] **Step 3: Remove the old reveal**

In `internal/game/rules.go`, delete these lines (~593-596) from the `MovePlayCard` branch:

```go
		// Reveal the mystery friend the moment the called card hits the table.
		if g.PartnerCard != nil && card.Suit == g.PartnerCard.Suit && card.Rank == g.PartnerCard.Rank {
			g.PartnerSeat = p.Seat
		}

```

- [ ] **Step 4: Add reveal at trick resolution + the helper**

In the same `MovePlayCard` branch, inside `if len(g.Tricks[idx].Cards) == 5 {`, immediately after:

```go
			winnerSeat, points := g.ResolveTrick(g.Tricks[idx])
			g.Tricks[idx].Winner = winnerSeat
```

add:

```go
			// Reveal the friend once they defend: they win a trick that holds a
			// scoring card, or take it with the joker. A pointless win stays
			// ambiguous, so it does not reveal.
			if g.PartnerSeat < 0 {
				if fs := g.friendSeat(); fs >= 0 && winnerSeat == fs && trickRevealsFriend(g.Tricks[idx], fs) {
					g.PartnerSeat = fs
				}
			}
```

Add the helper next to `friendSeat` (after it):

```go
// trickRevealsFriend reports whether winning this trick outs the friend: it
// holds a scoring card to defend, or the friend won it with the joker. (The
// mighty is an Ace, so it already counts as a scoring card.)
func trickRevealsFriend(t Trick, friendSeat int) bool {
	for _, played := range t.Cards {
		if played.Card.IsPointCard() {
			return true
		}

		if played.Seat == friendSeat && played.Card.Rank == Joker {
			return true
		}
	}

	return false
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/game/ -run 'Friend|Reveal|CalledCard'`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/game/rules.go internal/game/rules_friend_test.go
git commit -m "feat(game): reveal the friend when they defend a trick, not on calling the card"
```

---

### Task 3: Score the team via `friendSeat()`

**Files:**
- Modify: `internal/game/rules.go` — `CalculateFinalScore` (trick tally ~line 740, friend guard ~line 787) and the end-of-game `Scores` assignment in `ApplyMove` (~lines 643-644)
- Test: `internal/game/rules_test.go` (`TestScoring`, ~line 289) and `internal/game/rules_friend_test.go` (`TestScoringCountsRevealedPartnerTricks`, ~line 214)

**Interfaces:**
- Consumes: `g.friendSeat()` (Task 1).
- Produces: no new symbols; `CalculateFinalScore` and the finish block now attribute the friend's team membership via `friendSeat()` instead of `PartnerSeat`.

- [ ] **Step 1: Update the scoring tests to drive `friendSeat()`**

In `internal/game/rules_test.go`, in `TestScoring`, replace the setup block:

```go
	g := New("test-scoring")
	g.Declarer = 0
	g.PartnerSeat = 1
	g.Contract = &Bid{Points: 7, Suit: Spades, IsNoTrump: false}
```

with:

```go
	g := New("test-scoring")
	g.Declarer = 0
	// Friend is seat 1: place the called card in seat 1's hand so friendSeat()
	// resolves to 1 (scoring no longer reads PartnerSeat).
	g.PartnerCard = &Card{Suit: Hearts, Rank: King}
	g.Players[1] = &Player{ID: "p1", Seat: 1, Hand: []Card{{Suit: Hearts, Rank: King}}}
	g.Contract = &Bid{Points: 7, Suit: Spades, IsNoTrump: false}
```

Further down in the same test, the No-Friend section already sets `g.IsNoFriend = true` and `g.PartnerSeat = -1`; leave those lines — `IsNoFriend` drives the friend guard, and the `PartnerSeat = -1` line is now a harmless no-op.

In `internal/game/rules_friend_test.go`, in `TestScoringCountsRevealedPartnerTricks`, replace:

```go
			g.IsNoFriend = tc.noFriend
			g.PartnerSeat = 1
			if tc.noFriend {
				g.PartnerSeat = -1
			}
```

with:

```go
			g.IsNoFriend = tc.noFriend
			// Friend is seat 1: place the called card in seat 1's hand so
			// friendSeat() resolves to 1 (noFriend still forces -1).
			g.PartnerCard = &Card{Suit: Hearts, Rank: King}
			g.Players[1].Hand = []Card{{Suit: Hearts, Rank: King}}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/game/ -run 'TestScoring|TestScoringCountsRevealedPartnerTricks'`
Expected: FAIL — scoring still reads `PartnerSeat`, but the tests now leave `PartnerSeat` at its default (-1) and identify the friend via the called card, so the friend's tricks/half-score are not counted.

- [ ] **Step 3: Switch scoring to `friendSeat()`**

In `internal/game/rules.go`, in `CalculateFinalScore`, replace the tally block:

```go
	// Let's count tricks won by the caller team
	tricksWon := 0

	for _, t := range g.Tricks {
		if t.Winner == g.Declarer || t.Winner == g.PartnerSeat {
			tricksWon++
		}
	}
```

with:

```go
	fs := g.friendSeat()

	// Count tricks won by the caller team (declarer + friend).
	tricksWon := 0

	for _, t := range g.Tricks {
		if t.Winner == g.Declarer || t.Winner == fs {
			tricksWon++
		}
	}
```

and replace the friend-share guard:

```go
	friendScore := score / 2.0
	if g.IsNoFriend || g.PartnerSeat < 0 {
		friendScore = 0 // No revealed friend to share with.
	}
```

with:

```go
	friendScore := score / 2.0
	if g.IsNoFriend || fs < 0 {
		friendScore = 0 // No friend to share with.
	}
```

Then, in `ApplyMove`'s end-of-game block, replace:

```go
				if g.PartnerSeat >= 0 && g.PartnerSeat < len(g.Players) && g.Players[g.PartnerSeat] != nil {
					g.Scores[g.Players[g.PartnerSeat].ID] = int(partnerScore)
				}
```

with:

```go
				if fs := g.friendSeat(); fs >= 0 && fs < len(g.Players) && g.Players[fs] != nil {
					g.Scores[g.Players[fs].ID] = int(partnerScore)
				}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/game/`
Expected: PASS (whole game package, including all scoring and reveal tests).

- [ ] **Step 5: Commit**

```bash
git add internal/game/rules.go internal/game/rules_test.go internal/game/rules_friend_test.go
git commit -m "feat(game): attribute team scoring via friendSeat so an unrevealed friend still counts"
```

---

### Task 4: Update the e2e friend scenario to the new reveal semantics

**Files:**
- Modify: `tests/e2e/e2e_test.go` — add `seatThatPlayedCalledCard` helper (~near line 34/460); rewrite the "partner seat" step (~478-496) and the friend identification inside the "declarer-partner split" step (~499-527)
- Modify: `tests/e2e/features/friend.feature` (line 25)

**Interfaces:**
- Consumes: the backend reveal/scoring behavior from Tasks 1-3 (requires a rebuilt server image).
- Produces: e2e assertions robust to a friend who finishes the game unrevealed.

- [ ] **Step 1: Add the shared helper**

In `tests/e2e/e2e_test.go`, add a method on `apiFeature` (place it just before the step registrations that use it, e.g. above the `ctx.Step("^the partner seat ...")` block):

```go
// seatThatPlayedCalledCard returns the seat that played the declarer's called
// card across all tricks, or -1 if it was never played (no friend, or it was
// discarded into the kitty).
func (a *apiFeature) seatThatPlayedCalledCard() int {
	if a.calledCard == nil {
		return -1
	}

	for _, trick := range a.game.Tricks {
		for _, pc := range trick.Cards {
			if pc.Card.Suit == a.calledCard.Suit && pc.Card.Rank == a.calledCard.Rank {
				return pc.Seat
			}
		}
	}

	return -1
}
```

- [ ] **Step 2: Rewrite the "partner seat" step**

Replace the whole `ctx.Step(`^the partner seat should match whoever played the called card$`, ...)` registration with:

```go
	ctx.Step(`^the partner seat should be unrevealed or match whoever played the called card$`, func() error {
		if err := api.refreshState(); err != nil {
			return err
		}

		playedBy := api.seatThatPlayedCalledCard()
		if api.game.PartnerSeat != -1 && api.game.PartnerSeat != playedBy {
			return fmt.Errorf("partner seat %d, but called card was played by seat %d", api.game.PartnerSeat, playedBy)
		}

		return nil
	})
```

- [ ] **Step 3: Fix friend identification in the "declarer-partner split" step**

In the `ctx.Step(`^the final scores should follow the declarer-partner split$`, ...)` body, introduce the friend seat and use it in place of `api.game.PartnerSeat`. Replace this fragment:

```go
		if seat := api.game.PartnerSeat; seat >= 0 && seat != api.game.Declarer {
			partnerScore := api.game.Scores[api.game.Players[seat].ID]
			if diff := declarerScore - 2*partnerScore; diff < -1 || diff > 1 {
				return fmt.Errorf("partner score %d is not half of declarer %d", partnerScore, declarerScore)
			}
		}

		for _, p := range api.game.Players {
			if p == nil || p.Seat == api.game.Declarer || p.Seat == api.game.PartnerSeat {
				continue
			}

			if s := api.game.Scores[p.ID]; s != 0 {
				return fmt.Errorf("non-team player %d has score %d, want 0", p.Seat, s)
			}
		}
```

with:

```go
		friend := api.seatThatPlayedCalledCard()

		if friend >= 0 && friend != api.game.Declarer {
			partnerScore := api.game.Scores[api.game.Players[friend].ID]
			if diff := declarerScore - 2*partnerScore; diff < -1 || diff > 1 {
				return fmt.Errorf("partner score %d is not half of declarer %d", partnerScore, declarerScore)
			}
		}

		for _, p := range api.game.Players {
			if p == nil || p.Seat == api.game.Declarer || p.Seat == friend {
				continue
			}

			if s := api.game.Scores[p.ID]; s != 0 {
				return fmt.Errorf("non-team player %d has score %d, want 0", p.Seat, s)
			}
		}
```

- [ ] **Step 4: Update the feature file**

In `tests/e2e/features/friend.feature`, change line 25 from:

```gherkin
    And the partner seat should match whoever played the called card
```

to:

```gherkin
    And the partner seat should be unrevealed or match whoever played the called card
```

- [ ] **Step 5: Rebuild the stack and run the e2e suite**

Run:
```bash
docker compose up -d --build
go test -tags=integration ./tests/e2e/...
```
Expected: PASS (the `Mystery Friend` scenarios and the full marathon). If the compose stack is already running an old image, `--build` is required so the server includes Tasks 1-3.

- [ ] **Step 6: Commit**

```bash
git add tests/e2e/e2e_test.go tests/e2e/features/friend.feature
git commit -m "test(e2e): assert friend reveal-on-defense semantics and score the friend via the called card"
```

---

### Task 5: Whole-repo verification

**Files:** none (verification only).

- [ ] **Step 1: Full unit + vet**

Run: `go test ./... && go vet ./...`
Expected: PASS / clean.

- [ ] **Step 2: Lint (if available)**

Run: `golangci-lint run ./... 2>/dev/null || echo "golangci-lint not installed; skipping"`
Expected: clean, or the skip message. Address any new findings in the files this plan touched.

- [ ] **Step 3: Confirm e2e green against the rebuilt stack**

Run: `go test -tags=integration ./tests/e2e/...`
Expected: PASS. (No commit; verification only.)

---

## Self-Review

**Spec coverage:**
- `friendSeat()` helper (spec "New helper") → Task 1. ✓
- Remove called-card reveal; reveal on defense with point-card-or-joker rule (spec "Reveal detection") → Task 2. ✓
- Point card = `IsPointCard`, joker special case, opponent's point card defended, pointless win no reveal, non-friend no reveal, monotonic → Task 2 tests. ✓
- Scoring via `friendSeat()` (tally, guard, end-of-game Scores) (spec "Scoring correctness") → Task 3. ✓
- Edge cases: no-friend, discarded called card, self-partner (`friendSeat` returns declarer), monotonic → covered by `friendSeat` tests (Task 1), `TestDeclarerPlayingOwnCalledCardDoesNotRevealAlone` (Task 2), and preserved-behavior scoring (Task 3). ✓
- E2E reveal-step update (spec "Testing / E2E") → Task 4. ✓
- Out-of-scope raw-payload redaction → not implemented, matches spec. ✓

**Placeholder scan:** No TBD/TODO; every code step shows full code and exact commands. ✓

**Type consistency:** `friendSeat() int` and `trickRevealsFriend(Trick, int) bool` are defined in Tasks 1-2 and used with matching signatures in Tasks 2-3; the e2e helper `seatThatPlayedCalledCard() int` is defined and used within Task 4. `partner_seat` field and `PartnerSeat` name unchanged. ✓
