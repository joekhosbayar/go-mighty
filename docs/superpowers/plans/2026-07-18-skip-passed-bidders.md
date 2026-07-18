# Skip Passed Bidders Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure that players who have passed during the bidding phase remain passed and are skipped when the turn advances.

**Architecture:** We will modify the `ApplyMove` logic for `MoveBid` to stop resetting the `PassedPlayers` map. Then, in both `MoveBid` and `MovePass`, we will advance the turn using a loop that skips any player present in the `PassedPlayers` map.

**Tech Stack:** Go (Standard Library)

## Global Constraints

- Once a player passes during the bidding phase, they are permanently out of the auction.
- When advancing the turn after a bid or a pass, the game must automatically skip over any players who have already passed.

---

### Task 1: Skip Passed Bidders in ApplyMove

**Files:**
- Modify: `internal/game/rules.go`
- Modify: `internal/game/rules_test.go`

**Interfaces:**
- Consumes: `Game` struct and `ApplyMove` method.
- Produces: Correctly updated `g.CurrentTurn` skipping passed players.

- [ ] **Step 1: Write the failing test**

Append to `internal/game/rules_test.go`:
```go
func TestApplyMove_SkipPassedBidders(t *testing.T) {
	g := game.NewGame("test-game")
	// Add 5 players
	for i := 0; i < 5; i++ {
		g.AddPlayer(fmt.Sprintf("player%d", i+1), fmt.Sprintf("P%d", i+1))
	}
	g.Start()

	// Player 1 bids
	bid1 := game.Bid{Suit: game.Clubs, Points: 3}
	err := g.ApplyMove("player1", game.MoveBid, bid1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Player 2 passes
	err = g.ApplyMove("player2", game.MovePass, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	// Player 3 passes
	err = g.ApplyMove("player3", game.MovePass, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Player 4 bids
	bid2 := game.Bid{Suit: game.Diamonds, Points: 4}
	err = g.ApplyMove("player4", game.MoveBid, bid2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// After player 4 bids, it should be player 5's turn
	if g.CurrentTurn != 4 {
		t.Errorf("Expected current turn 4 (player 5), got %d", g.CurrentTurn)
	}

	// Player 5 passes
	err = g.ApplyMove("player5", game.MovePass, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now it should skip player 2 and 3 and be player 1's turn
	if g.CurrentTurn != 0 {
		t.Errorf("Expected current turn 0 (player 1) after skipping, got %d", g.CurrentTurn)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game -run TestApplyMove_SkipPassedBidders`
Expected: FAIL with "Expected current turn 0 (player 1) after skipping, got 1"

- [ ] **Step 3: Write minimal implementation**

In `internal/game/rules.go` inside `ApplyMove`:
For `case MoveBid:`, remove:
```go
		g.PassedPlayers = make(map[int]bool) // Clear passes when someone bids
```
And replace the turn advancement for `MoveBid`:
```go
		} else {
			g.CurrentTurn = (g.CurrentTurn + 1) % 5
		}
```
with:
```go
		} else {
			for {
				g.CurrentTurn = (g.CurrentTurn + 1) % 5
				if !g.PassedPlayers[g.CurrentTurn] {
					break
				}
			}
		}
```

For `case MovePass:`, replace the turn advancement:
```go
		g.CurrentTurn = (g.CurrentTurn + 1) % 5
```
with:
```go
		for {
			g.CurrentTurn = (g.CurrentTurn + 1) % 5
			if !g.PassedPlayers[g.CurrentTurn] {
				break
			}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/game -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/game/rules.go internal/game/rules_test.go
git commit -m "fix: skip passed players when advancing turn in bidding phase"
```
