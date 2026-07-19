# Four-Player Mighty Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the four-player Mighty variant (43-card deck, min bid 14, configurable 2-vs-2 failure payouts, optional joker-partner) alongside the existing five-player game, in the `go-mighty` engine and `mighty-frontend` client, without changing any five-player behavior.

**Architecture:** A single additive `GameConfig` value is carried on `Game`. Every place that today hardcodes `5`, `13`, or the deck shape reads config-derived helpers (`g.numSeats()`, `g.minBidPoints()`) instead. `New(id)` keeps its signature by delegating to a new `NewWithConfig(id, cfg)` that defaults to five-player, so existing callers and tests compile untouched. The frontend gains a create-game config form and reads `game.config` for seat count / bid minimum.

**Tech Stack:** Go 1.22+ (`internal/game`, `internal/service`, `internal/api`), React + TypeScript + Vitest (`mighty-frontend/src`).

## Global Constraints

- Five-player behavior must be byte-for-byte unchanged. The five-player regression suite (`go test ./internal/game/...`) must stay green after every task.
- `Points` is the existing 3–10 bid scale; scoring-card `target = Points + 10`.
- Minimum bid: five-player `Points >= 3` (target 13); four-player `Points >= 4` (target 14). Maximum stays `Points == 10` (target 20).
- Four-player deck is exactly 43 cards: the 53-card deck minus all four `2`s, all four `4`s, and the two red `3`s (`{Hearts,"3"}`, `{Diamonds,"3"}`). All 20 point cards (A,K,Q,J,10) remain.
- Both variants deal 10 cards per player + 3 kitty and play exactly 10 tricks.
- `FailDist` values are the exact strings `"equal_split"`, `"declarer_alone"`, `"two_one_split"`. Default is `equal_split`.
- Four-player games occupy seats 0..NumPlayers-1; seat 4 stays `nil` for a four-player game. `Players` stays `[5]*Player`.
- All scoring result maps sum to zero and contain only integers.

---

## File Structure

**Backend (`go-mighty`):**
- `internal/game/config.go` (create) — `GameConfig`, `FailDist`, `DefaultConfig()`, helpers `numSeats()`, `minBidPoints()`.
- `internal/game/game.go` (modify) — add `Config` field; `New`/`NewWithConfig`; `IsFull`; `Start` deals via config.
- `internal/game/card.go` (modify) — `NewDeckFor(numPlayers)`, `Deal(numPlayers)`.
- `internal/game/rules.go` (modify) — rotation/trick/bidding-end over `numSeats()`; bid minimum; joker-partner rule; `CalculateFinalScore` M + failure branch.
- `internal/game/*_test.go` (modify/create) — new tests + a four-player scoring helper.
- `internal/service/game_service.go` (modify) — `CreateGame(ctx, id, cfg)`, seat loop bounded to `numSeats()`.
- `internal/api/handler.go` (modify) — `GameService.CreateGame` signature; parse create-game body into `GameConfig`.
- test mocks / callers of `CreateGame` (modify) — pass config.

**Frontend (`mighty-frontend`):**
- `src/core/types.ts` (modify) — `GameConfig` type + `config` on `Game`.
- `src/api/http.ts` (modify) — `createGame(token, config)`.
- `src/components/LobbyScreen.tsx` (modify) — create-game config form; `/N seated`.
- `src/components/BidPanel.tsx` (modify) — minimum from config.
- `src/components/ScoreBoard.tsx` (modify) — iterate seated players.

---

## Task 1: GameConfig type + helpers

**Files:**
- Create: `internal/game/config.go`
- Modify: `internal/game/game.go` (struct field + `New`)
- Test: `internal/game/config_test.go`

**Interfaces:**
- Produces: `type FailDist string`; consts `FailEqualSplit="equal_split"`, `FailDeclarerAlone="declarer_alone"`, `FailTwoOneSplit="two_one_split"`. `type GameConfig struct { NumPlayers int; AllowJokerPartner bool; FailDist FailDist }`. `func DefaultConfig() GameConfig` → `{5, true, FailEqualSplit}`. Methods `func (g *Game) numSeats() int`, `func (g *Game) minBidPoints() int`. `func NewWithConfig(id string, cfg GameConfig) *Game`; `New(id)` unchanged externally.

- [ ] **Step 1: Write the failing test**

Create `internal/game/config_test.go`:

```go
package game

import "testing"

func TestDefaultConfigIsFivePlayer(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.NumPlayers != 5 || !cfg.AllowJokerPartner || cfg.FailDist != FailEqualSplit {
		t.Fatalf("unexpected default config: %+v", cfg)
	}
}

func TestNewDefaultsToFivePlayer(t *testing.T) {
	g := New("cfg")
	if g.numSeats() != 5 {
		t.Errorf("numSeats: got %d, want 5", g.numSeats())
	}
	if g.minBidPoints() != 3 {
		t.Errorf("minBidPoints: got %d, want 3", g.minBidPoints())
	}
}

func TestNewWithConfigFourPlayer(t *testing.T) {
	g := NewWithConfig("cfg4", GameConfig{NumPlayers: 4, AllowJokerPartner: false, FailDist: FailTwoOneSplit})
	if g.numSeats() != 4 {
		t.Errorf("numSeats: got %d, want 4", g.numSeats())
	}
	if g.minBidPoints() != 4 {
		t.Errorf("minBidPoints: got %d, want 4", g.minBidPoints())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-mighty && go test ./internal/game/ -run 'TestDefaultConfig|TestNew' -v`
Expected: FAIL / build error — `DefaultConfig`, `NewWithConfig`, `numSeats`, `minBidPoints` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `internal/game/config.go`:

```go
package game

// FailDist selects how losses are split when a four-player 2-vs-2 contract fails.
type FailDist string

const (
	// FailEqualSplit: declarer -S, partner -S, each opponent +S.
	FailEqualSplit FailDist = "equal_split"
	// FailDeclarerAlone: declarer -2S, partner 0, each opponent +S.
	FailDeclarerAlone FailDist = "declarer_alone"
	// FailTwoOneSplit: partner -S, each opponent +ceil(1.5S); declarer pays the
	// remainder (-2S, or -(2S+1) when S is odd so the amounts stay integers).
	FailTwoOneSplit FailDist = "two_one_split"
)

// GameConfig captures every difference between the four- and five-player games.
type GameConfig struct {
	NumPlayers        int      `json:"num_players"`
	AllowJokerPartner bool     `json:"allow_joker_partner"`
	FailDist          FailDist `json:"fail_dist"`
}

// DefaultConfig returns the standard five-player configuration.
func DefaultConfig() GameConfig {
	return GameConfig{NumPlayers: 5, AllowJokerPartner: true, FailDist: FailEqualSplit}
}

// numSeats is the number of players this game seats (4 or 5).
func (g *Game) numSeats() int {
	if g.Config.NumPlayers == 0 {
		return 5
	}
	return g.Config.NumPlayers
}

// minBidPoints is the lowest legal bid on the 3-10 scale: 3 (target 13) for
// five players, 4 (target 14) for four players.
func (g *Game) minBidPoints() int {
	if g.numSeats() == 4 {
		return 4
	}
	return 3
}
```

In `internal/game/game.go`, add the field to the `Game` struct (immediately after the `Status` field, near line 81):

```go
	Config  GameConfig `json:"config"`
```

Replace the `New` function (lines 131-148) with:

```go
// New creates a new five-player game instance.
func New(id string) *Game {
	return NewWithConfig(id, DefaultConfig())
}

// NewWithConfig creates a new game with the given configuration.
func NewWithConfig(id string, cfg GameConfig) *Game {
	if cfg.NumPlayers == 0 {
		cfg.NumPlayers = 5
	}
	if cfg.FailDist == "" {
		cfg.FailDist = FailEqualSplit
	}
	g := &Game{
		ID:            id,
		Status:        PhaseWaiting,
		Config:        cfg,
		Players:       [5]*Player{},
		PassedPlayers: make(map[int]bool),
		Tricks:        make([]Trick, 0),
		Scores:        make(map[string]int),
		Declarer:      -1,
		PartnerSeat:   -1,
		Version:       1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	return g
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-mighty && go test ./internal/game/ -run 'TestDefaultConfig|TestNew' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
cd go-mighty && git add internal/game/config.go internal/game/config_test.go internal/game/game.go
git commit -m "feat(game): add GameConfig with numSeats/minBidPoints helpers"
```

---

## Task 2: Config-aware deck & deal

**Files:**
- Modify: `internal/game/card.go` (`NewDeck` lines 98-114, `Deal` lines 123-154)
- Modify: `internal/game/card_test.go` (existing `Deal()` callers, if any)
- Test: `internal/game/card_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `func NewDeckFor(numPlayers int) Deck`; `NewDeck() Deck` retained (= `NewDeckFor(5)`). `func (d Deck) Deal(numPlayers int) ([][]Card, []Card)` — replaces the old zero-arg `Deal() ([5][]Card, []Card)`.

- [ ] **Step 1: Write the failing test**

Add to `internal/game/card_test.go`:

```go
func TestNewDeckForFourPlayerHas43Cards(t *testing.T) {
	d := NewDeckFor(4)
	if len(d) != 43 {
		t.Fatalf("four-player deck size: got %d, want 43", len(d))
	}
	removed := map[Card]bool{
		{Hearts, Three}: true, {Diamonds, Three}: true,
	}
	pointCards := 0
	for _, c := range d {
		if c.Rank == Two || c.Rank == Four {
			t.Errorf("2s and 4s must be removed, found %s", c)
		}
		if removed[c] {
			t.Errorf("red 3 must be removed, found %s", c)
		}
		if c.IsPointCard() {
			pointCards++
		}
	}
	if pointCards != 20 {
		t.Errorf("point cards: got %d, want 20", pointCards)
	}
	// Black 3s stay.
	hasBlackThrees := 0
	for _, c := range d {
		if c.Rank == Three && (c.Suit == Spades || c.Suit == Clubs) {
			hasBlackThrees++
		}
	}
	if hasBlackThrees != 2 {
		t.Errorf("black 3s: got %d, want 2", hasBlackThrees)
	}
}

func TestNewDeckForFivePlayerHas53Cards(t *testing.T) {
	if len(NewDeckFor(5)) != 53 {
		t.Fatalf("five-player deck size: got %d, want 53", len(NewDeckFor(5)))
	}
}

func TestDealFourPlayer(t *testing.T) {
	d := NewDeckFor(4)
	hands, kitty := d.Deal(4)
	if len(hands) != 4 {
		t.Fatalf("hands: got %d, want 4", len(hands))
	}
	for i, h := range hands {
		if len(h) != 10 {
			t.Errorf("hand %d: got %d cards, want 10", i, len(h))
		}
	}
	if len(kitty) != 3 {
		t.Errorf("kitty: got %d, want 3", len(kitty))
	}
}

func TestDealFivePlayer(t *testing.T) {
	hands, kitty := NewDeckFor(5).Deal(5)
	if len(hands) != 5 || len(kitty) != 3 {
		t.Fatalf("five-player deal shape wrong: %d hands, %d kitty", len(hands), len(kitty))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-mighty && go test ./internal/game/ -run 'NewDeckFor|TestDeal' -v`
Expected: FAIL / build error — `NewDeckFor` undefined and `Deal` arity mismatch.

- [ ] **Step 3: Write minimal implementation**

In `internal/game/card.go`, replace `NewDeck` (lines 98-114) with:

```go
// NewDeck creates a standard 53-card deck (52 + 1 Joker) for five players.
func NewDeck() Deck {
	return NewDeckFor(5)
}

// NewDeckFor builds the deck for the given player count: 53 cards for five
// players, or 43 for four players (all 2s, all 4s, and the two red 3s removed).
func NewDeckFor(numPlayers int) Deck {
	suits := []Suit{Spades, Diamonds, Hearts, Clubs}
	ranks := []Rank{Ace, King, Queen, Jack, Ten, Nine, Eight, Seven, Six, Five, Four, Three, Two}

	deck := make(Deck, 0, 53)
	for _, s := range suits {
		for _, r := range ranks {
			if numPlayers == 4 {
				if r == Two || r == Four {
					continue
				}
				if r == Three && (s == Hearts || s == Diamonds) {
					continue
				}
			}
			deck = append(deck, Card{Suit: s, Rank: r})
		}
	}
	deck = append(deck, Card{Suit: None, Rank: Joker})
	return deck
}
```

Replace `Deal` (lines 123-154) with:

```go
// Deal distributes 10 cards to each of numPlayers players and 3 to the kitty.
// Returns the hands (one slice per player) and the kitty.
func (d Deck) Deal(numPlayers int) ([][]Card, []Card) {
	expected := numPlayers*10 + 3
	if len(d) != expected {
		return nil, nil
	}

	hands := make([][]Card, numPlayers)
	k := 0
	for i := 0; i < numPlayers; i++ {
		hands[i] = make([]Card, 10)
		for j := 0; j < 10; j++ {
			hands[i][j] = d[k]
			k++
		}
	}

	kitty := make([]Card, 3)
	copy(kitty, d[k:])
	return hands, kitty
}
```

If `internal/game/card_test.go` has any existing call to `Deal()` with no argument, change it to `Deal(5)` and update the return from `[5][]Card` indexing (slice indexing works unchanged).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-mighty && go test ./internal/game/ -run 'NewDeckFor|TestDeal' -v`
Expected: PASS (4 tests). (Full package will not build yet because `Start` still calls the old `Deal()`; that is fixed in Task 3. Run only the `-run` filter here.)

- [ ] **Step 5: Commit**

```bash
cd go-mighty && git add internal/game/card.go internal/game/card_test.go
git commit -m "feat(game): config-aware NewDeckFor and Deal for four-player deck"
```

---

## Task 3: Deal wiring + IsFull over numSeats

**Files:**
- Modify: `internal/game/game.go` (`IsFull` lines 150-161, `Start` lines 163-184)
- Test: `internal/game/game_deal_test.go` (create)

**Interfaces:**
- Consumes: `g.numSeats()` (Task 1); `NewDeckFor`, `Deal(numPlayers)` (Task 2).
- Produces: `IsFull()` true at `numSeats()` filled; `Start()` deals the config deck into seats `0..numSeats()-1`.

- [ ] **Step 1: Write the failing test**

Create `internal/game/game_deal_test.go`:

```go
package game

import "testing"

func TestFourPlayerStartDealsFourHands(t *testing.T) {
	g := NewWithConfig("deal4", GameConfig{NumPlayers: 4, AllowJokerPartner: true, FailDist: FailEqualSplit})
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i}
	}
	if !g.IsFull() {
		t.Fatal("four seated players should fill a four-player game")
	}
	g.Start()
	for i := 0; i < 4; i++ {
		if got := len(g.Players[i].Hand); got != 10 {
			t.Errorf("seat %d hand: got %d, want 10", i, got)
		}
	}
	if g.Players[4] != nil {
		t.Error("seat 4 must stay nil in a four-player game")
	}
	if len(g.Kitty) != 3 {
		t.Errorf("kitty: got %d, want 3", len(g.Kitty))
	}
	if g.Status != PhaseBidding {
		t.Errorf("status: got %s, want %s", g.Status, PhaseBidding)
	}
}

func TestFivePlayerIsFullStillFive(t *testing.T) {
	g := New("full5")
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i}
	}
	if g.IsFull() {
		t.Fatal("four of five seats should not be full")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-mighty && go test ./internal/game/ -run 'FourPlayerStart|FivePlayerIsFull' -v`
Expected: FAIL / build error — package does not compile because `Start` calls `deck.Deal()` (old arity) and `hands` is `[5][]Card`.

- [ ] **Step 3: Write minimal implementation**

In `internal/game/game.go`, replace `IsFull` (lines 150-161):

```go
// IsFull checks if the game has all its seats filled.
func (g *Game) IsFull() bool {
	count := 0
	for i := 0; i < g.numSeats(); i++ {
		if g.Players[i] != nil {
			count++
		}
	}
	return count == g.numSeats()
}
```

Replace the deal portion of `Start` (lines 164-176) so it uses the config deck. The new body:

```go
// Start deals the cards and starts the bidding phase.
func (g *Game) Start() {
	deck := NewDeckFor(g.numSeats())
	deck.Shuffle()
	hands, kitty := deck.Deal(g.numSeats())

	for i, h := range hands {
		if g.Players[i] != nil {
			g.Players[i].Hand = h
			g.Players[i].Points = []Card{}
		}
	}

	g.Kitty = kitty
	g.Status = PhaseBidding

	g.CurrentTurn = 0 // Seat 0 bids first
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-mighty && go test ./internal/game/ -run 'FourPlayerStart|FivePlayerIsFull' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Run the full game package to confirm five-player regressions**

Run: `cd go-mighty && go test ./internal/game/`
Expected: PASS (whole package compiles and all existing tests green).

- [ ] **Step 6: Commit**

```bash
cd go-mighty && git add internal/game/game.go internal/game/game_deal_test.go
git commit -m "feat(game): deal config deck and seat-count IsFull"
```

---

## Task 4: Turn/trick/bidding-end + bid minimum over numSeats

**Files:**
- Modify: `internal/game/rules.go` — bid minimum (line 114-115); play rotation (line 660); trick complete (line 663); pass/bid end conditions (lines 516, 534, 545); `advanceToNextBidder` (lines 898-909)
- Test: `internal/game/rules_fourplayer_test.go` (create)

**Interfaces:**
- Consumes: `g.numSeats()`, `g.minBidPoints()`.
- Produces: four-player rotation and bidding termination; four-player bids below `Points 4` rejected.

- [ ] **Step 1: Write the failing test**

Create `internal/game/rules_fourplayer_test.go`:

```go
package game

import (
	"errors"
	"testing"
)

func fourPlayerBidding(t *testing.T) *Game {
	t.Helper()
	g := NewWithConfig("bid4", GameConfig{NumPlayers: 4, AllowJokerPartner: true, FailDist: FailEqualSplit})
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i, Hand: []Card{}}
	}
	g.Start()
	return g
}

func TestFourPlayerBidBelowFourteenRejected(t *testing.T) {
	g := fourPlayerBidding(t)
	err := g.ValidateMove(g.Players[0].ID, MoveBid, Bid{Points: 3, Suit: Spades})
	if !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove for Points 3 in four-player game, got %v", err)
	}
	if err := g.ValidateMove(g.Players[0].ID, MoveBid, Bid{Points: 4, Suit: Spades}); err != nil {
		t.Fatalf("Points 4 should be legal in four-player game, got %v", err)
	}
}

func TestFourPlayerBiddingEndsWhenThreePass(t *testing.T) {
	g := fourPlayerBidding(t)
	_ = g.ApplyMove(g.Players[0].ID, MoveBid, Bid{Points: 4, Suit: Spades})
	_ = g.ApplyMove(g.Players[1].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[2].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[3].ID, MovePass, nil)
	if g.Status != PhaseExchanging {
		t.Fatalf("status after three passes: got %s, want %s", g.Status, PhaseExchanging)
	}
	if g.Declarer != 0 {
		t.Errorf("declarer: got %d, want 0", g.Declarer)
	}
	if len(g.Players[0].Hand) != 13 {
		t.Errorf("declarer hand after kitty: got %d, want 13", len(g.Players[0].Hand))
	}
}

func TestFourPlayerAllPassRedeals(t *testing.T) {
	g := fourPlayerBidding(t)
	_ = g.ApplyMove(g.Players[0].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[1].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[2].ID, MovePass, nil)
	_ = g.ApplyMove(g.Players[3].ID, MovePass, nil)
	if g.Status != PhaseBidding {
		t.Fatalf("all-pass should redeal into bidding, got %s", g.Status)
	}
	if len(g.PassedPlayers) != 0 {
		t.Errorf("passed players should reset, got %d", len(g.PassedPlayers))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-mighty && go test ./internal/game/ -run 'FourPlayerBid|FourPlayerBidding|FourPlayerAllPass' -v`
Expected: FAIL — Points 3 is currently accepted; bidding-end fires at 4 passes not 3; all-pass redeal keyed to 5.

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go`:

Replace the bid-minimum check (lines 114-116):

```go
	if bid.Points < g.minBidPoints() || bid.Points > 10 {
		return fmt.Errorf("%w: bid points must be between %d and 10", ErrInvalidMove, g.minBidPoints())
	}
```

Replace the bid auto-resolve condition (line 516):

```go
		if bid.Points == 10 || len(g.PassedPlayers) == g.numSeats()-1 {
```

Replace the pass end-of-bidding conditions (lines 534 and 545):

```go
		if len(g.PassedPlayers) == g.numSeats()-1 && g.CurrentBid != nil {
```

and

```go
		} else if len(g.PassedPlayers) == g.numSeats() {
```

Replace the play rotation (line 660):

```go
		g.CurrentTurn = (g.CurrentTurn + 1) % g.numSeats()
```

Replace the trick-complete check (line 663):

```go
		if len(g.Tricks[idx].Cards) == g.numSeats() {
```

Replace `advanceToNextBidder` (lines 898-909):

```go
// advanceToNextBidder advances the current turn to the next player who has not passed.
func (g *Game) advanceToNextBidder() {
	if len(g.PassedPlayers) >= g.numSeats() {
		return
	}
	for {
		g.CurrentTurn = (g.CurrentTurn + 1) % g.numSeats()
		if !g.PassedPlayers[g.CurrentTurn] {
			break
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-mighty && go test ./internal/game/ -run 'FourPlayer' -v`
Expected: PASS.

- [ ] **Step 5: Run full game package (five-player regression)**

Run: `cd go-mighty && go test ./internal/game/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd go-mighty && git add internal/game/rules.go internal/game/rules_fourplayer_test.go
git commit -m "feat(game): generalize rotation, trick, and bidding-end over numSeats"
```

---

## Task 5: Joker-as-partner toggle

**Files:**
- Modify: `internal/game/rules.go` (`validateCallPartner`, lines 217-224)
- Test: `internal/game/rules_fourplayer_test.go` (append)

**Interfaces:**
- Consumes: `g.Config.NumPlayers`, `g.Config.AllowJokerPartner`.
- Produces: a Joker partner call rejected in four-player games when `!AllowJokerPartner`; allowed otherwise and always in five-player.

- [ ] **Step 1: Write the failing test**

Append to `internal/game/rules_fourplayer_test.go`:

```go
func callPartnerGame(t *testing.T, allowJoker bool) *Game {
	t.Helper()
	g := NewWithConfig("joker4", GameConfig{NumPlayers: 4, AllowJokerPartner: allowJoker, FailDist: FailEqualSplit})
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: string(rune('A' + i)), Seat: i}
	}
	g.Declarer = 0
	g.Status = PhaseCalling
	return g
}

func TestFourPlayerJokerPartnerRejectedWhenDisallowed(t *testing.T) {
	g := callPartnerGame(t, false)
	joker := Card{Suit: None, Rank: Joker}
	err := g.ValidateMove(g.Players[0].ID, MoveCallPartner, CallPartnerMove{Card: &joker})
	if !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("joker partner should be rejected when disallowed, got %v", err)
	}
}

func TestFourPlayerJokerPartnerAllowedWhenEnabled(t *testing.T) {
	g := callPartnerGame(t, true)
	joker := Card{Suit: None, Rank: Joker}
	if err := g.ValidateMove(g.Players[0].ID, MoveCallPartner, CallPartnerMove{Card: &joker}); err != nil {
		t.Fatalf("joker partner should be allowed when enabled, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-mighty && go test ./internal/game/ -run 'FourPlayerJokerPartner' -v`
Expected: FAIL — `TestFourPlayerJokerPartnerRejectedWhenDisallowed` accepts the joker (no rule yet).

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go`, replace the partner-card validation block (lines 217-224):

```go
	if move.Card != nil {
		isJoker := move.Card.Suit == None && move.Card.Rank == Joker
		if isJoker {
			if g.Config.NumPlayers == 4 && !g.Config.AllowJokerPartner {
				return fmt.Errorf("%w: joker may not be called as partner in this game", ErrInvalidMove)
			}
		} else {
			if _, ok := suitRank[move.Card.Suit]; !ok || !validRanks[move.Card.Rank] {
				return fmt.Errorf("%w: invalid partner card", ErrInvalidMove)
			}
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-mighty && go test ./internal/game/ -run 'FourPlayerJokerPartner' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
cd go-mighty && git add internal/game/rules.go internal/game/rules_fourplayer_test.go
git commit -m "feat(game): optional joker-as-partner rule for four-player games"
```

---

## Task 6: Scoring engine — M generalization + failure distribution

**Files:**
- Modify: `internal/game/rules.go` (`CalculateFinalScore`, lines 782-860)
- Test: `internal/game/rules_fourscore_test.go` (create)

**Interfaces:**
- Consumes: `g.minBidPoints()`, `g.numSeats()`, `g.Config.NumPlayers`, `g.Config.FailDist`, `g.friendSeat()`.
- Produces: `CalculateFinalScore()` correct for four players; five-player output unchanged.

- [ ] **Step 1: Write the failing test**

Create `internal/game/rules_fourscore_test.go`:

```go
package game

import (
	"fmt"
	"testing"
)

// fourScoringGame builds a finished four-player hand. Seat 0 is declarer; seat 1
// holds the called partner card unless noFriend. Seats 0-3 filled; seat 4 nil.
func fourScoringGame(bid int, fd FailDist, noFriend bool, teamPoints int) *Game {
	g := NewWithConfig("score4", GameConfig{NumPlayers: 4, AllowJokerPartner: true, FailDist: fd})
	g.Declarer = 0
	g.Contract = &Bid{Points: bid, Suit: Spades}
	for i := 0; i < 4; i++ {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Seat: i}
	}
	if noFriend {
		g.IsNoFriend = true
	} else {
		g.PartnerCard = &Card{Suit: Hearts, Rank: King}
		g.Players[1].Hand = []Card{{Suit: Hearts, Rank: King}}
	}
	g.Players[0].Points = make([]Card, teamPoints)
	return g
}

func TestFourPlayerScore(t *testing.T) {
	tests := []struct {
		name                              string
		bid                               int
		fd                                FailDist
		noFriend                          bool
		teamPoints                        int
		wantDecl, wantPartner, wantOppEa  int
	}{
		// success: s = 2*(5-4) + (16-15) = 3; win split declarer +3 partner +3 opp -3
		{"success 15s 16pts", 5, FailEqualSplit, false, 16, 3, 3, -3},
		// fail equal_split: s = 15-13 = 2; declarer -2 partner -2 opp +2
		{"fail equal 13pts", 5, FailEqualSplit, false, 13, -2, -2, 2},
		// fail declarer_alone: s=2; declarer -4 partner 0 opp +2
		{"fail declarer-alone 13pts", 5, FailDeclarerAlone, false, 13, -4, 0, 2},
		// fail two_one even: s=2; opp +3, partner -2, declarer -4
		{"fail two-one even 13pts", 5, FailTwoOneSplit, false, 13, -4, -2, 3},
		// fail two_one odd: s = 15-12 = 3; opp +5, partner -3, declarer -7
		{"fail two-one odd 12pts", 5, FailTwoOneSplit, false, 12, -7, -3, 5},
		// alone success: s = 2*(5-4)+(16-15)=3, doubled by the no-friend ×2 = 6;
		// declarer +18 vs three opponents -6
		{"alone success 16pts", 5, FailEqualSplit, true, 16, 18, 0, -6},
		// alone fail: s = 15-13 = 2, doubled by no-friend ×2 = 4; declarer -12,
		// opp +4 (fd ignored when alone)
		{"alone fail 13pts", 5, FailTwoOneSplit, true, 13, -12, 0, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := fourScoringGame(tc.bid, tc.fd, tc.noFriend, tc.teamPoints)
			got := g.CalculateFinalScore()
			if got[0] != tc.wantDecl {
				t.Errorf("declarer: got %d, want %d", got[0], tc.wantDecl)
			}
			if !tc.noFriend && got[1] != tc.wantPartner {
				t.Errorf("partner: got %d, want %d", got[1], tc.wantPartner)
			}
			// Opponents: seats 2,3 always; seat 1 too when alone.
			oppSeats := []int{2, 3}
			if tc.noFriend {
				oppSeats = []int{1, 2, 3}
			}
			for _, s := range oppSeats {
				if got[s] != tc.wantOppEa {
					t.Errorf("opp seat %d: got %d, want %d", s, got[s], tc.wantOppEa)
				}
			}
			sum := 0
			for _, v := range got {
				sum += v
			}
			if sum != 0 {
				t.Errorf("scores must sum to zero, got %d (%v)", sum, got)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-mighty && go test ./internal/game/ -run TestFourPlayerScore -v`
Expected: FAIL — current formula uses `2*(Points-3)` (wrong M) and has no failure-distribution branch.

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go`, within `CalculateFinalScore`, replace the success value line (line 815):

```go
		s = 2*(g.Contract.Points-g.minBidPoints()) + (p - target)
```

Then replace the entire distribution loop (lines 834-857, from `sign := 1` through the closing brace of the `for` loop) with:

```go
	sign := 1
	if !success {
		sign = -1
	}
	partnerShare := 0
	if partnerPresent {
		partnerShare = s
	}

	// A configured four-player 2-vs-2 failure uses a special split; every other
	// case (all wins, all alone games, and every five-player result) uses the
	// standard formula where each opponent pays S, the partner collects S, and
	// the declarer collects the remainder, with signs flipped on failure.
	special := !success && partnerPresent && g.Config.NumPlayers == 4 &&
		(g.Config.FailDist == FailDeclarerAlone || g.Config.FailDist == FailTwoOneSplit)

	if special {
		var declarerPay, partnerPay, oppGain int
		switch g.Config.FailDist {
		case FailDeclarerAlone:
			declarerPay, partnerPay, oppGain = 2*s, 0, s
		default: // FailTwoOneSplit
			oppGain = (3*s + 1) / 2            // ceil(1.5*s)
			partnerPay = s
			declarerPay = 2*oppGain - partnerPay // 2s if s even, 2s+1 if s odd
		}
		for seat, player := range g.Players {
			if player == nil {
				continue
			}
			switch {
			case seat == declarer:
				scores[seat] = -declarerPay
			case seat == fs:
				scores[seat] = -partnerPay
			default:
				scores[seat] = oppGain
			}
		}
		return scores
	}

	// Standard distribution (sums to zero).
	for seat, player := range g.Players {
		if player == nil {
			continue
		}
		switch {
		case seat == declarer:
			scores[seat] = sign * (oppCount*s - partnerShare)
		case partnerPresent && seat == fs:
			scores[seat] = sign * s
		default:
			scores[seat] = -sign * s
		}
	}

	return scores
}
```

Note: the existing function already ends with `return scores` after the loop; make sure there is exactly one trailing `return scores` / closing brace after this replacement (the block above already includes the final `return scores` and the function's closing `}`).

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-mighty && go test ./internal/game/ -run TestFourPlayerScore -v`
Expected: PASS (7 subtests).

- [ ] **Step 5: Run the existing five-player scoring suite (regression)**

Run: `cd go-mighty && go test ./internal/game/ -run TestCalculateFinalScore -v`
Expected: PASS (all existing rows unchanged).

- [ ] **Step 6: Run full game package**

Run: `cd go-mighty && go test ./internal/game/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd go-mighty && git add internal/game/rules.go internal/game/rules_fourscore_test.go
git commit -m "feat(game): four-player scoring with M=14 and configurable fail payout"
```

---

## Task 7: Service + API wiring for game config

**Files:**
- Modify: `internal/service/game_service.go` (`CreateGame` lines 69-84; `JoinGame` seat loop lines 128-134)
- Modify: `internal/api/handler.go` (`GameService` interface line 22; `CreateGameHandler` lines 139-166)
- Modify: any `CreateGame` mock/caller in `internal/api/*_test.go` and `internal/service/*_test.go`
- Test: `internal/service/game_service_config_test.go` (create) or extend existing service test

**Interfaces:**
- Consumes: `game.GameConfig`, `game.NewWithConfig`, `g.numSeats()`.
- Produces: `CreateGame(ctx, id string, cfg game.GameConfig) (*game.Game, error)`; `CreateGameHandler` parses an optional JSON body `{num_players, allow_joker_partner, fail_dist}` and defaults to five-player when absent/empty.

- [ ] **Step 1: Write the failing test**

Create `internal/service/game_service_config_test.go` (adjust the constructor call to match the existing service test setup helper in `game_service_test.go` — reuse its store/mocks):

```go
package service

import (
	"context"
	"testing"

	"github.com/joekhosbayar/go-mighty/internal/game"
)

func TestCreateGameStoresFourPlayerConfig(t *testing.T) {
	// newTestService is the helper used by game_service_test.go; reuse it.
	svc := newTestService(t)
	cfg := game.GameConfig{NumPlayers: 4, AllowJokerPartner: false, FailDist: game.FailTwoOneSplit}
	g, err := svc.CreateGame(context.Background(), "cfg-game", cfg)
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	if g.Config.NumPlayers != 4 || g.Config.AllowJokerPartner || g.Config.FailDist != game.FailTwoOneSplit {
		t.Fatalf("config not stored: %+v", g.Config)
	}
}
```

If `game_service_test.go` has no `newTestService` helper, replace the first line with the same construction the existing tests use (mirror their setup exactly). Verify by reading `internal/service/game_service_test.go` first.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd go-mighty && go test ./internal/service/ -run TestCreateGameStoresFourPlayerConfig -v`
Expected: FAIL / build error — `CreateGame` takes 2 args, not 3.

- [ ] **Step 3: Write minimal implementation**

In `internal/service/game_service.go`, change `CreateGame` (lines 69-71):

```go
// CreateGame initializes a new game and persists it in both Postgres and Redis.
func (s *Game) CreateGame(ctx context.Context, id string, cfg game.GameConfig) (*game.Game, error) {
	g := game.NewWithConfig(id, cfg)
```

In `JoinGame`, bound the "first available seat" loop to the configured seats (replace lines 128-134):

```go
	// If not already in the game, find the first available seat within the
	// configured number of seats.
	for i := 0; i < g.NumSeatsPublic(); i++ {
		if g.Players[i] == nil {
			seat = i
			break
		}
	}
```

`numSeats()` is unexported. Add a small exported accessor in `internal/game/config.go`:

```go
// NumSeatsPublic exposes the seat count to other packages.
func (g *Game) NumSeatsPublic() int { return g.numSeats() }
```

In `internal/api/handler.go`, update the interface method (line 22):

```go
	CreateGame(ctx context.Context, id string, cfg game.GameConfig) (*game.Game, error)
```

Replace `CreateGameHandler` body around the create call (lines 146-153) so it parses an optional config body and defaults sensibly:

```go
	actualID := uuid.NewString()

	cfg := game.DefaultConfig()
	if r.Body != nil {
		var req struct {
			NumPlayers        int    `json:"num_players"`
			AllowJokerPartner *bool  `json:"allow_joker_partner"`
			FailDist          string `json:"fail_dist"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if req.NumPlayers == 4 || req.NumPlayers == 5 {
				cfg.NumPlayers = req.NumPlayers
			}
			if req.AllowJokerPartner != nil {
				cfg.AllowJokerPartner = *req.AllowJokerPartner
			}
			switch game.FailDist(req.FailDist) {
			case game.FailEqualSplit, game.FailDeclarerAlone, game.FailTwoOneSplit:
				cfg.FailDist = game.FailDist(req.FailDist)
			}
		}
	}

	// Create the game
	g, err := h.svc.CreateGame(r.Context(), actualID, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
```

Update every other caller/mock of `CreateGame`:
- Search: `cd go-mighty && grep -rn "CreateGame(" internal cmd | grep -v "_test.go:.*func"`.
- For a mock implementing `GameService`, change its `CreateGame` method signature to accept `cfg game.GameConfig` (ignore it or record it).
- For any real caller passing 2 args, add `game.DefaultConfig()`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd go-mighty && go test ./internal/service/ -run TestCreateGameStoresFourPlayerConfig -v`
Expected: PASS.

- [ ] **Step 5: Build and test the whole backend (compile + regression)**

Run: `cd go-mighty && go build ./... && go test ./...`
Expected: PASS (all packages compile; existing suites green).

- [ ] **Step 6: Commit**

```bash
cd go-mighty && git add internal/service/game_service.go internal/api/handler.go internal/game/config.go internal/service/game_service_config_test.go
# plus any modified mock/caller files surfaced by the grep in step 3
git commit -m "feat(api): accept and persist game config on create"
```

---

## Task 8: Frontend — config types + createGame payload

**Files:**
- Modify: `mighty-frontend/src/core/types.ts` (`Game` interface, lines 42-66)
- Modify: `mighty-frontend/src/api/http.ts` (`Http` interface line 22; `createGame` line 50)
- Test: `mighty-frontend/src/api/http.test.ts`

**Interfaces:**
- Produces: `GameConfig` TS type; `config?: GameConfig` on `Game`; `createGame(token: string, config?: GameConfig): Promise<Game>` posting the config body.

- [ ] **Step 1: Write the failing test**

Add to `mighty-frontend/src/api/http.test.ts` (mirror the file's existing fetch-mock style; adapt names to the existing helpers there):

```ts
it('posts game config to createGame', async () => {
  let captured: RequestInit | undefined
  const fetchFn = (async (_url: string, init?: RequestInit) => {
    captured = init
    return new Response(JSON.stringify({ id: 'g1' }), { status: 200 })
  }) as unknown as typeof fetch
  const http = createHttp(fetchFn)
  await http.createGame('tok', { num_players: 4, allow_joker_partner: false, fail_dist: 'two_one_split' })
  expect(JSON.parse(String(captured?.body))).toEqual({
    num_players: 4,
    allow_joker_partner: false,
    fail_dist: 'two_one_split',
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mighty-frontend && npx vitest run src/api/http.test.ts`
Expected: FAIL — `createGame` currently posts `{}` and takes one argument.

- [ ] **Step 3: Write minimal implementation**

In `mighty-frontend/src/core/types.ts`, add above the `Game` interface:

```ts
export type FailDist = 'equal_split' | 'declarer_alone' | 'two_one_split'

export interface GameConfig {
  num_players: number
  allow_joker_partner: boolean
  fail_dist: FailDist
}
```

And add to the `Game` interface (after `id`):

```ts
  config?: GameConfig
```

In `mighty-frontend/src/api/http.ts`, update the interface (line 22):

```ts
  createGame(token: string, config?: GameConfig): Promise<Game>
```

Import the type at the top of the file (extend the existing `types` import), then replace the `createGame` implementation (line 50):

```ts
    createGame: (token, config) => request('/games', post(config ?? {}, token)),
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mighty-frontend && npx vitest run src/api/http.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd mighty-frontend && git add src/core/types.ts src/api/http.ts src/api/http.test.ts
git commit -m "feat(frontend): game config type and createGame payload"
```

---

## Task 9: Frontend — create-table config form + seat count

**Files:**
- Modify: `mighty-frontend/src/components/LobbyScreen.tsx` (`onCreate` prop line 8; button line 37; `/5 seated` line 55)
- Test: `mighty-frontend/src/components/LobbyScreen.test.tsx`

**Interfaces:**
- Consumes: `GameConfig` (Task 8).
- Produces: `LobbyScreen` `onCreate(config: GameConfig)`; a form to pick 4/5 players and (for 4p) `fail_dist` + `allow_joker_partner`; seated count reads `game.config?.num_players ?? 5`.

- [ ] **Step 1: Write the failing test**

Add to `mighty-frontend/src/components/LobbyScreen.test.tsx` (mirror existing render/query helpers in that file):

```tsx
it('creates a four-player table with chosen fail rule', async () => {
  const onCreate = vi.fn()
  render(
    <LobbyScreen games={[]} username="u" onCreate={onCreate} onJoin={() => {}} onRefresh={() => {}} onLogout={() => {}} />,
  )
  await userEvent.selectOptions(screen.getByLabelText(/players/i), '4')
  await userEvent.selectOptions(screen.getByLabelText(/failure/i), 'two_one_split')
  await userEvent.click(screen.getByRole('button', { name: /create table/i }))
  expect(onCreate).toHaveBeenCalledWith(
    expect.objectContaining({ num_players: 4, fail_dist: 'two_one_split' }),
  )
})

it('shows seated count out of configured player count', () => {
  const game = {
    id: 'g', status: 'waiting', config: { num_players: 4, allow_joker_partner: true, fail_dist: 'equal_split' },
    players: [{ id: 'a', name: 'A', seat: 0 }, null, null, null], created_at: new Date().toISOString(),
  } as never
  render(
    <LobbyScreen games={[game]} username="u" onCreate={() => {}} onJoin={() => {}} onRefresh={() => {}} onLogout={() => {}} />,
  )
  expect(screen.getByText(/1\/4 seated/)).toBeInTheDocument()
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mighty-frontend && npx vitest run src/components/LobbyScreen.test.tsx`
Expected: FAIL — no player/failure selects; `onCreate` takes no args; seated count hardcoded `/5`.

- [ ] **Step 3: Write minimal implementation**

In `mighty-frontend/src/components/LobbyScreen.tsx`:

Change the prop type (line 8):

```tsx
  onCreate(config: GameConfig): void
```

Import `GameConfig` from `../core/types`. Add local form state inside the component (near the top of the function body):

```tsx
  const [numPlayers, setNumPlayers] = useState(5)
  const [failDist, setFailDist] = useState<GameConfig['fail_dist']>('equal_split')
  const [allowJoker, setAllowJoker] = useState(true)
```

Replace the create button (line 37) with a small form:

```tsx
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <label>Players
            <select aria-label="players" value={numPlayers} onChange={e => setNumPlayers(Number(e.target.value))}>
              <option value={5}>5</option>
              <option value={4}>4</option>
            </select>
          </label>
          {numPlayers === 4 && (
            <>
              <label>Failure rule
                <select aria-label="failure rule" value={failDist} onChange={e => setFailDist(e.target.value as GameConfig['fail_dist'])}>
                  <option value="equal_split">Equal split</option>
                  <option value="declarer_alone">Declarer pays alone</option>
                  <option value="two_one_split">2x / 1x split</option>
                </select>
              </label>
              <label>
                <input type="checkbox" checked={allowJoker} onChange={e => setAllowJoker(e.target.checked)} /> Allow joker partner
              </label>
            </>
          )}
          <button
            onClick={() => onCreate({ num_players: numPlayers, allow_joker_partner: numPlayers === 5 ? true : allowJoker, fail_dist: failDist })}
            style={{ background: 'var(--color-accent)', color: 'var(--color-ink)', fontWeight: 'bold' }}
          >
            Create Table
          </button>
        </div>
```

Replace the seated-count span (line 55):

```tsx
                  <span style={{ color: 'var(--color-accent)' }}>{`${g.players.filter(Boolean).length}/${g.config?.num_players ?? 5} seated`}</span>
```

Ensure `useState` is imported from `react`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mighty-frontend && npx vitest run src/components/LobbyScreen.test.tsx`
Expected: PASS.

- [ ] **Step 5: Update the create-table caller**

Search the caller of `LobbyScreen` (likely `src/routes/LobbyRoute.tsx` or `App.tsx`) for `onCreate`: it must now pass the config through to `http.createGame(token, config)`. Update that wiring so `onCreate={cfg => createGame(cfg)}`.

Run: `cd mighty-frontend && grep -rn "onCreate" src/routes src/App.tsx`
Then update the handler to accept and forward the config, and run `npx vitest run` on the affected route/App test file to confirm green.

- [ ] **Step 6: Commit**

```bash
cd mighty-frontend && git add src/components/LobbyScreen.tsx src/components/LobbyScreen.test.tsx
# plus the updated route/App caller
git commit -m "feat(frontend): create-table config form and seated count"
```

---

## Task 10: Frontend — bid minimum + scoreboard over configured players

**Files:**
- Modify: `mighty-frontend/src/components/BidPanel.tsx` (initial `points` line 21; options line 36)
- Modify: `mighty-frontend/src/components/ScoreBoard.tsx`
- Test: `mighty-frontend/src/components/BidPanel.test.tsx`, `mighty-frontend/src/components/ScoreBoard.test.tsx`

**Interfaces:**
- Consumes: `game.config?.num_players`.
- Produces: BidPanel minimum bid derived from config (3 for five-player, 4 for four-player); ScoreBoard iterates only seated players.

- [ ] **Step 1: Write the failing test**

Add to `mighty-frontend/src/components/BidPanel.test.tsx` (mirror the file's existing `view`/props construction):

```tsx
it('offers minimum bid 4 for a four-player game', () => {
  renderBidPanel({ config: { num_players: 4, allow_joker_partner: true, fail_dist: 'equal_split' } })
  const options = screen.getAllByRole('option').map(o => Number((o as HTMLOptionElement).value))
  expect(Math.min(...options)).toBe(4)
})

it('offers minimum bid 3 for a five-player game', () => {
  renderBidPanel({})
  const options = screen.getAllByRole('option').map(o => Number((o as HTMLOptionElement).value))
  expect(Math.min(...options)).toBe(3)
})
```

Add a `renderBidPanel(gameOverrides)` helper if the file lacks one, building the minimal `view`/`game` props the panel needs (read the existing test to match its prop shape).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mighty-frontend && npx vitest run src/components/BidPanel.test.tsx`
Expected: FAIL — options are always `[3..10]`.

- [ ] **Step 3: Write minimal implementation**

In `mighty-frontend/src/components/BidPanel.tsx`, derive the minimum from config. Add near the top of the component (the panel already has access to the game/view — use whichever prop carries `config`; if only `view` is passed, thread `config` through `view` or add a `minBid` prop from the parent). Concretely:

```tsx
  const minBid = view.config?.num_players === 4 ? 4 : 3
  const [points, setPoints] = useState(minBid)
```

Replace the options map (line 36):

```tsx
            {Array.from({ length: 11 - minBid }, (_, i) => minBid + i).map(n => (
```

If `view` does not currently expose `config`, add `config` to the view type in `src/core/view.ts` (pass it straight through from `game.config`) so the panel can read it; update `view.ts` and its test accordingly in this step.

- [ ] **Step 4: Run BidPanel test to verify it passes**

Run: `cd mighty-frontend && npx vitest run src/components/BidPanel.test.tsx`
Expected: PASS.

- [ ] **Step 5: Write the failing ScoreBoard test**

Add to `mighty-frontend/src/components/ScoreBoard.test.tsx` (match existing prop shape):

```tsx
it('renders one row per seated player in a four-player game', () => {
  const game = {
    config: { num_players: 4, allow_joker_partner: true, fail_dist: 'equal_split' },
    players: [
      { id: 'a', name: 'A', seat: 0 }, { id: 'b', name: 'B', seat: 1 },
      { id: 'c', name: 'C', seat: 2 }, { id: 'd', name: 'D', seat: 3 }, null,
    ],
    scores: { a: 3, b: 3, c: -3, d: -3 },
  } as never
  renderScoreBoard(game)
  expect(screen.getAllByTestId('score-row')).toHaveLength(4)
})
```

Ensure each rendered player row has `data-testid="score-row"` (add it in the next step if missing).

- [ ] **Step 6: Run to verify it fails, then implement**

Run: `cd mighty-frontend && npx vitest run src/components/ScoreBoard.test.tsx`
Expected: FAIL (renders 5 rows or the testid is missing).

In `ScoreBoard.tsx`, filter to seated players before mapping and tag each row:

```tsx
      {players.filter(Boolean).map(p => (
        <div key={p!.id} data-testid="score-row">
          {/* existing row content, using p! */}
        </div>
      ))}
```

(Replace `players` with however the component currently accesses the roster; the key change is `.filter(Boolean)` so the nil seat 4 is skipped, plus the `data-testid`.)

- [ ] **Step 7: Run both component tests**

Run: `cd mighty-frontend && npx vitest run src/components/BidPanel.test.tsx src/components/ScoreBoard.test.tsx`
Expected: PASS.

- [ ] **Step 8: Full frontend test + lint**

Run: `cd mighty-frontend && npx vitest run && npx tsc -p tsconfig.app.json --noEmit`
Expected: PASS (all tests green, no type errors).

- [ ] **Step 9: Commit**

```bash
cd mighty-frontend && git add src/components/BidPanel.tsx src/components/BidPanel.test.tsx src/components/ScoreBoard.tsx src/components/ScoreBoard.test.tsx src/core/view.ts src/core/view.test.ts
git commit -m "feat(frontend): config-driven bid minimum and scoreboard"
```

---

## Task 11: End-to-end verification

**Files:**
- No production changes expected. Fix-forward only if a gap surfaces.

- [ ] **Step 1: Backend full suite**

Run: `cd go-mighty && go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 2: Backend lint**

Run: `cd go-mighty && golangci-lint run ./internal/...` (if `golangci-lint` is installed; otherwise `go vet ./...`)
Expected: no new findings.

- [ ] **Step 3: Frontend full suite + typecheck + lint**

Run: `cd mighty-frontend && npx vitest run && npx tsc -p tsconfig.app.json --noEmit && npx oxlint`
Expected: PASS.

- [ ] **Step 4: Manual four-player smoke (optional but recommended)**

Start the stack per `go-mighty/readme.md` (docker-compose), create a four-player table via the UI, seat four players, confirm: bidding rejects below 14, deal is 43 cards / 10 per hand, a completed hand produces a zero-sum scoreboard matching the chosen failure rule.

- [ ] **Step 5: Final commit (if any fixups were needed)**

```bash
git add -A && git commit -m "test: end-to-end verification fixups for four-player mode"
```

---

## Self-Review Notes (traceability)

- Spec §1 GameConfig → Task 1. §2 Deck/deal → Tasks 2-3. §3 player-count generalization + bid min + joker rule → Tasks 4-5. §4 scoring (M + fail dist) → Task 6. §5 wiring/frontend → Tasks 7-10. §6 testing → embedded per task + Task 11.
- Five-player regression guarded in Tasks 3, 4, 6, 7, and 11.
- `FailTwoOneSplit` odd/even integer edge covered by Task 6 rows "fail two-one even" (s=2) and "fail two-one odd" (s=3); every scoring row asserts sum-to-zero.
- Alone (no-partner) path ignores `FailDist` — covered by Task 6 "alone fail" row using `FailTwoOneSplit` yet expecting the standard alone payout.
