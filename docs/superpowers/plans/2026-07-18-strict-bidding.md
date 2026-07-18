# Strict Bidding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix a bidding logic bug so that a new bid must be strictly greater in points than the current bid, and automatically resolve the bidding phase when a 10-point bid is made.

**Architecture:** We will simplify `validateBid` in `rules.go` to enforce strictly increasing point values and update `ApplyMove` to automatically transition the phase to `PhaseExchanging` if a valid bid of 10 points is applied.

**Tech Stack:** Go

## Global Constraints

- A new bid must be strictly greater than the current bid's points.
- If a player submits a bid of 10 points (the maximum allowable bid), the bidding phase immediately ends and resolves, skipping any remaining passes.

---

### Task 1: Enforce Strict Point Increases in Bidding

**Files:**
- Modify: `internal/game/rules.go`
- Modify: `internal/game/rules_test.go`

**Interfaces:**
- Consumes: Existing `Game.validateBid` signature.
- Produces: A stricter validation rule rejecting equal or lesser point bids.

- [ ] **Step 1: Write the failing test**

In `internal/game/rules_test.go`, append this new test block to the end of the file:

```go
func TestValidateBid_StrictlyIncreasing(t *testing.T) {
	t.Parallel()
	g := New("test-strict-bid")
	g.Status = PhaseBidding
	g.CurrentTurn = 0
	g.Players[0] = &Player{ID: "P1", Seat: 0}

	// Given a current bid of 7 Clubs
	g.CurrentBid = &Bid{Points: 7, Suit: Clubs, IsNoTrump: false}

	// A bid of 7 Spades (higher suit rank) should be rejected
	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 7, Suit: Spades, IsNoTrump: false}); err == nil {
		t.Fatalf("expected 7 Spades over 7 Clubs to be rejected (points must be strictly higher)")
	}

	// A bid of 7 No-Trump should be rejected
	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 7, Suit: None, IsNoTrump: true}); err == nil {
		t.Fatalf("expected 7 NT over 7 Clubs to be rejected (points must be strictly higher)")
	}

	// A bid of 8 Clubs should be accepted
	if err := g.ValidateMove("P1", MoveBid, Bid{Points: 8, Suit: Clubs, IsNoTrump: false}); err != nil {
		t.Fatalf("expected 8 Clubs over 7 Clubs to be accepted, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/game -run TestValidateBid_StrictlyIncreasing`
Expected: FAIL with "expected 7 Spades over 7 Clubs to be rejected"

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go`, locate `validateBid`. Replace lines 128-149 with:

```go
	// Must be higher than current bid
	if g.CurrentBid != nil {
		if bid.Points <= g.CurrentBid.Points {
			return fmt.Errorf("%w: bid must be strictly higher than current bid", ErrInvalidMove)
		}
	}
```

- [ ] **Step 4: Update existing tests**

In `internal/game/rules_test.go`, locate `TestGameFlow`. Modify lines 54-58 to test points rather than suits:

```go
	// Player 4 attempts a same-point bid; should be rejected
	err = g.ValidateMove(g.Players[4].ID, MoveBid, Bid{Points: 7, Suit: Spades})
	if err == nil {
		t.Errorf("Expected error for same-point bid")
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -v ./internal/game`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/game/rules.go internal/game/rules_test.go
git commit -m "fix: enforce strictly increasing point values in bids"
```

---

### Task 2: Auto-Resolve Bidding on Maximum Bid

**Files:**
- Modify: `internal/game/rules.go`
- Modify: `internal/game/rules_test.go`

**Interfaces:**
- Consumes: Existing `Game.ApplyMove` behavior for `MoveBid`.
- Produces: Instant state transition when bid is 10.

- [ ] **Step 1: Write the failing test**

In `internal/game/rules_test.go`, append this new test to the end of the file:

```go
func TestApplyMove_MaxBidAutoResolves(t *testing.T) {
	t.Parallel()
	g := New("test-max-bid")
	g.Start() // deals cards and sets PhaseBidding

	playerID := g.Players[g.CurrentTurn].ID
	err := g.ApplyMove(playerID, MoveBid, Bid{Points: 10, Suit: Spades, IsNoTrump: false})
	if err != nil {
		t.Fatalf("failed to apply 10 point bid: %v", err)
	}

	if g.Status != PhaseExchanging {
		t.Fatalf("expected phase to immediately become PhaseExchanging, got %s", g.Status)
	}
	if g.Declarer != g.GetPlayer(playerID).Seat {
		t.Fatalf("expected declarer to be set correctly")
	}
	if g.Contract == nil || g.Contract.Points != 10 {
		t.Fatalf("expected contract to be finalized")
	}
	if len(g.Kitty) != 0 {
		t.Fatalf("expected kitty to be emptied into declarer's hand")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/game -run TestApplyMove_MaxBidAutoResolves`
Expected: FAIL with "expected phase to immediately become PhaseExchanging"

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go`, inside `ApplyMove` under the `case MoveBid:` section (around line 431), replace:

```go
		// In rotation, move turn to next player?
		// Or if everyone passes?
		// Simplified: We assume bidding continues until 4 passes?
		// For now simple implementation: Just set bid and move turn.
		g.CurrentTurn = (g.CurrentTurn + 1) % 5
```

With:

```go
		if bid.Points == 10 {
			// Auto-resolve if maximum bid is reached
			g.Status = PhaseExchanging
			g.Contract = g.CurrentBid
			g.Trump = g.Contract.Suit
			g.CurrentTurn = g.Declarer

			declarer := g.Players[g.Declarer]
			declarer.Hand = append(declarer.Hand, g.Kitty...)
			g.Kitty = nil
		} else {
			g.CurrentTurn = (g.CurrentTurn + 1) % 5
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -v ./internal/game -run TestApplyMove_MaxBidAutoResolves`
Expected: PASS

- [ ] **Step 5: Run all tests to verify no regressions**

Run: `go test -v ./internal/game`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/game/rules.go internal/game/rules_test.go
git commit -m "feat: auto-resolve bidding phase when a 10-point bid is made"
```
