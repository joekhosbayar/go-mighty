# Official Mighty Scoring Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the trick-based "UCLA Campus Standard" scoring with the official Mighty regulation — scoring on captured scoring cards, correct formulas and doublings, and a fully zero-sum per-seat payout.

**Architecture:** Rewrite `Game.CalculateFinalScore` to return a `map[int]int` (seat → signed round score, summing to zero) computed from each team member's captured scoring cards (`Player.Points`). A single distribution rule ("each opponent pays S, partner collects S, declarer collects the rest, signs flip on failure") produces the regulation payouts for both partnered and solo play. The end-of-game call site writes that per-seat map straight into `g.Scores`.

**Tech Stack:** Go (backend, `internal/game`), TypeScript/React + Vitest (frontend display).

## Global Constraints

- Bids are entered on the **3–10 scale**; the scoring-card target is **`target = bid + 10`** (official 13–20 scale). `P` (captured scoring cards) is the raw 0–20 count and is **never** shifted.
- Success formula: `S = 2×(bid − 3) + (P − target)`. Failure formula: `S = target − P`.
- Doublings each multiply `S` by 2, stacking multiplicatively: **run** (`P == 20`), **back-run** (`20 − P ≥ 11`), **no-trump** (`Contract.IsNoTrump`), **no-friend** (`IsNoFriend`, announced solo only).
- Payout is **strictly zero-sum** across all five seats. No ±800 cap. Integer math only.
- Secret-solo (called card in declarer's own hand or kitty, so `friendSeat()` is `-1` or `== declarer`) gets the alone distribution but **no** no-friend double.

---

### Task 1: Rewrite the scoring engine and its call site

**Files:**
- Modify: `internal/game/rules.go:787-850` (replace `CalculateFinalScore`)
- Modify: `internal/game/rules.go:683-704` (the end-of-game block that calls it)
- Test: `internal/game/rules_test.go:289-350` (replace `TestScoring`)

**Interfaces:**
- Consumes: `Game.Contract *Bid` (fields `Points int`, `IsNoTrump bool`), `Game.IsNoFriend bool`, `Game.Declarer int`, `Game.friendSeat() int`, `Game.Players [5]*Player` (each `*Player` has `ID string`, `Points []Card`).
- Produces: `func (g *Game) CalculateFinalScore() map[int]int` — seat index → signed round score, summing to zero; empty map when `g.Contract == nil`.

- [ ] **Step 1: Replace `TestScoring` with the new table-driven test**

Replace the whole `TestScoring` function (`internal/game/rules_test.go:289-350`) with:

```go
// scoringGame builds a finished-hand game where the caller team has captured
// `teamPoints` scoring cards. Seats 0-4 are all filled. Seat 0 is the declarer.
// With a friend, seat 1 holds the called card; secretSolo puts it in seat 0's
// own hand so friendSeat() resolves to the declarer.
func scoringGame(bid int, noTrump, noFriend, secretSolo bool, teamPoints int) *Game {
	g := New("score")
	g.Declarer = 0
	g.Contract = &Bid{Points: bid, Suit: Spades, IsNoTrump: noTrump}
	for i := 0; i < 5; i++ {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Seat: i}
	}
	switch {
	case noFriend:
		g.IsNoFriend = true
	case secretSolo:
		g.PartnerCard = &Card{Suit: Hearts, Rank: King}
		g.Players[0].Hand = []Card{{Suit: Hearts, Rank: King}}
	default:
		g.PartnerCard = &Card{Suit: Hearts, Rank: King}
		g.Players[1].Hand = []Card{{Suit: Hearts, Rank: King}}
	}
	// All the team's captured scoring cards sit on the declarer's pile; only
	// the length is read, so zero-value cards are fine.
	g.Players[0].Points = make([]Card, teamPoints)
	return g
}

func TestCalculateFinalScore(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                    string
		bid                     int
		noTrump, noFriend, solo bool
		teamPoints              int
		wantDeclarer            int
		wantPartner             int // meaningful only when a partner exists
		wantOpp                 int
	}{
		{"success 15d 16pts", 5, false, false, false, 16, 10, 5, -5},
		{"fail 15d 13pts", 5, false, false, false, 13, -4, -2, 2},
		{"success 16nt 18pts", 6, true, false, false, 18, 32, 16, -16},
		{"fail 16nt 13pts", 6, true, false, false, 13, -12, -6, 6},
		{"run 17h 20pts", 7, false, false, false, 20, 44, 22, -22},
		{"alone 16nt 17pts", 6, true, true, false, 17, 112, 0, -28},
		{"alone fail 16nt 15pts", 6, true, true, false, 15, -16, 0, 4},
		{"zero payment bid3 13pts", 3, false, false, false, 13, 0, 0, 0},
		{"back run bid5 9pts", 5, false, false, false, 9, -24, -12, 12},
		{"secret solo 15s 16pts", 5, false, false, true, 16, 20, 0, -5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := scoringGame(tc.bid, tc.noTrump, tc.noFriend, tc.solo, tc.teamPoints)
			got := g.CalculateFinalScore()

			if got[0] != tc.wantDeclarer {
				t.Errorf("declarer: got %d, want %d", got[0], tc.wantDeclarer)
			}
			if !tc.noFriend && !tc.solo && got[1] != tc.wantPartner {
				t.Errorf("partner: got %d, want %d", got[1], tc.wantPartner)
			}
			// Every opponent seat pays/collects the same amount.
			for _, seat := range oppSeats(g) {
				if got[seat] != tc.wantOpp {
					t.Errorf("opp seat %d: got %d, want %d", seat, got[seat], tc.wantOpp)
				}
			}
			// Zero-sum invariant.
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

// oppSeats returns the seats that are neither the declarer nor a revealed partner.
func oppSeats(g *Game) []int {
	declarer := g.Declarer
	fs := g.friendSeat()
	partner := fs >= 0 && fs != declarer
	var seats []int
	for seat, p := range g.Players {
		if p == nil || seat == declarer || (partner && seat == fs) {
			continue
		}
		seats = append(seats, seat)
	}
	return seats
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd go-mighty && go test ./internal/game/ -run TestCalculateFinalScore`
Expected: **build failure** — `CalculateFinalScore` still returns `(float64, float64)`, so `got[0]` and the single-value assignment don't compile (`assignment mismatch` / `cannot index`). The call site at `rules.go:685` also breaks the build. This is the red state.

- [ ] **Step 3: Rewrite `CalculateFinalScore`**

Replace the entire function body at `internal/game/rules.go:787-850` with:

```go
// CalculateFinalScore computes each seat's signed round score under the
// official Mighty regulations. The returned map is keyed by seat index and
// always sums to zero. Bids are on the 3-10 scale; the scoring-card target is
// bid + 10. P is the caller team's captured scoring cards.
func (g *Game) CalculateFinalScore() map[int]int {
	scores := make(map[int]int)
	if g.Contract == nil {
		return scores
	}

	declarer := g.Declarer
	fs := g.friendSeat()
	partnerPresent := fs >= 0 && fs != declarer
	onTeam := func(seat int) bool {
		return seat == declarer || (partnerPresent && seat == fs)
	}

	// P: the team's captured scoring cards. All 20 point cards are always
	// distributed - trick points go to winners, kitty discards to the declarer.
	p := 0
	oppCount := 0
	for seat, player := range g.Players {
		if player == nil {
			continue
		}
		if onTeam(seat) {
			p += len(player.Points)
		} else {
			oppCount++
		}
	}

	target := g.Contract.Points + 10
	success := p >= target

	var s int
	if success {
		s = 2*(g.Contract.Points-3) + (p - target)
	} else {
		s = target - p
	}

	// Doublings, each multiplicative.
	if p == 20 { // run
		s *= 2
	}
	if 20-p >= 11 { // back run: defenders took >= 11 scoring cards
		s *= 2
	}
	if g.Contract.IsNoTrump {
		s *= 2
	}
	if g.IsNoFriend {
		s *= 2
	}

	sign := 1
	if !success {
		sign = -1
	}
	partnerShare := 0
	if partnerPresent {
		partnerShare = s
	}

	// Distribution (sums to zero): each opponent pays S, the partner collects S,
	// the declarer collects the remainder. Every sign flips on failure.
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

- [ ] **Step 4: Update the end-of-game call site**

Replace `internal/game/rules.go:683-704` (the `if len(g.Tricks) == 10 { ... } else { ... }` block) with:

```go
			if len(g.Tricks) == 10 {
				g.Status = PhaseFinished
				seatScores := g.CalculateFinalScore()

				// Zero-sum per-seat result for the round, keyed by player ID.
				g.Scores = make(map[string]int, len(g.Players))
				for seat, player := range g.Players {
					if player != nil {
						g.Scores[player.ID] = seatScores[seat]
					}
				}
			} else {
				g.Tricks = append(g.Tricks, Trick{Cards: []PlayedCard{}})
			}
```

- [ ] **Step 5: Run the scoring test to verify it passes**

Run: `cd go-mighty && go test ./internal/game/ -run TestCalculateFinalScore -v`
Expected: PASS — all 10 subtests green.

- [ ] **Step 6: Run the full backend suite**

Run: `cd go-mighty && go test ./...`
Expected: PASS. The e2e step "final scores should follow the declarer-partner split" (`tests/e2e/e2e_test.go:525`) still holds — the declarer's `2S` is exactly twice the partner's `S`. If any other test asserts an old trick-based value, update it to the official value using the formulas in Global Constraints.

- [ ] **Step 7: Commit**

```bash
cd go-mighty
git add internal/game/rules.go internal/game/rules_test.go
git commit -m "fix: official Mighty scoring - card-based, zero-sum payouts"
```

---

### Task 2: Update the rules documentation

**Files:**
- Modify: `docs/rules.md:56-63` (the "Scoring (UCLA Campus Standard)" section)

- [ ] **Step 1: Replace the scoring section**

Replace `docs/rules.md:56-63` with:

```markdown
## Scoring (Official Mighty)

Scores are zero-sum: they add up to zero across all five players. `P` is the number
of the 20 scoring cards the declarer's team captured. Bids are entered on a 3–10
scale, mapping to a scoring-card target of `bid + 10` (13–20).

- **Success** (`P ≥ bid + 10`): `S = 2×(bid − 3) + (P − (bid + 10))`.
- **Failure** (`P < bid + 10`): `S = (bid + 10) − P`.
- **Distribution** — success: declarer **+2S**, partner **+S**, each opponent **−S**;
  failure reverses every sign. Playing alone: declarer **±4S**, each opponent **∓S**.
- **Multipliers** (each ×2, stacking):
    - **Run**: team captured all 20 scoring cards.
    - **Back-run**: opponents captured ≥ 11 scoring cards.
    - **No-Trump**.
    - **No-Friend**: announced solo only (not a secret solo).
- If the bid is the minimum (3) and the team takes exactly 13 points, `S = 0` and
  there is no payment.
```

- [ ] **Step 2: Commit**

```bash
cd go-mighty
git add docs/rules.md
git commit -m "docs: rules.md scoring section to official Mighty rules"
```

---

### Task 3: Render negative round scores in the frontend

The backend now sends negative round scores for opponents. `view.ts` already maps each player's score through, and `types.ts` infers `roundScore` as a plain `number`, so nothing breaks functionally — this task only adds a signed/colored display so losses read clearly.

**Files:**
- Modify: `mighty-frontend/src/components/ScoreBoard.tsx:29` (the round-score cell)
- Test: `mighty-frontend/src/components/ScoreBoard.test.tsx`

**Interfaces:**
- Consumes: `ScoreRow.roundScore: number` (may be negative, zero, or positive) from `view.ts`.

- [ ] **Step 1: Add a failing test for signed round-score display**

Add this test inside the `describe('ScoreBoard', ...)` block in `mighty-frontend/src/components/ScoreBoard.test.tsx`:

```ts
it('shows a negative round score with a minus sign', () => {
  const g = buildFinishedGame({
    scores: { p0: 10, p1: 5, p2: -5, p3: -5, p4: -5 },
  })
  render(<ScoreBoard view={tableView(g, 'p2')} />)
  expect(screen.getByText('-5')).toBeInTheDocument()
})
```

If the existing test's game builder is not named `buildFinishedGame`/does not accept `scores`, reuse whatever builder the sibling test at `ScoreBoard.test.tsx:19` uses (it passes `scores: { p0: 50, p1: 25 }`) and give three players a negative value.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd mighty-frontend && npx vitest run src/components/ScoreBoard.test.tsx`
Expected: FAIL — a negative value is not rendered distinctly / the asserted node isn't found, OR (if `-5` already renders as plain text) confirm the test passes trivially; in that case still complete Step 3 to add the color cue and assert on it instead:

```ts
expect(screen.getByText('-5')).toHaveStyle({ color: 'var(--color-loss)' })
```

- [ ] **Step 3: Color the round-score cell by sign**

At `mighty-frontend/src/components/ScoreBoard.tsx:29`, wrap the round-score value so negatives are visually distinct. Replace the cell's content with:

```tsx
<td
  style={{
    fontFamily: 'var(--font-mono)',
    fontSize: '1.1rem',
    color:
      row.roundScore < 0
        ? 'var(--color-loss)'
        : row.roundScore > 0
          ? 'var(--color-gain)'
          : 'inherit',
  }}
>
  {row.roundScore > 0 ? `+${row.roundScore}` : row.roundScore}
</td>
```

If `--color-loss` / `--color-gain` are not defined in the app's CSS variables, use literal `'#c0392b'` (loss) and `'#2e7d32'` (gain) instead.

- [ ] **Step 4: Run the frontend suite**

Run: `cd mighty-frontend && npx vitest run src/components/ScoreBoard.test.tsx src/core/view.test.ts`
Expected: PASS. The existing `view.test.ts` and `ScoreBoard.test.tsx` assertions use their own made-up score values and remain valid.

- [ ] **Step 5: Commit**

```bash
cd mighty-frontend
git add src/components/ScoreBoard.tsx src/components/ScoreBoard.test.tsx
git commit -m "feat: show signed round scores in ScoreBoard"
```

---

## Notes for the implementer

- The whole point of Task 1 is that **P counts scoring cards, not tricks.** `Player.Points` already accumulates captured point cards, including the declarer's three kitty discards (`rules.go:590`). Do not reintroduce any `Tricks` counting into scoring.
- `friendSeat()` returns `-1` for an announced no-friend or a card buried in the kitty, and returns the declarer's own seat for a called card the declarer holds. The `partnerPresent := fs >= 0 && fs != declarer` guard handles all three correctly — no extra special-casing.
- Keep everything integer. There is no half-split and no ±800 cap anymore.
