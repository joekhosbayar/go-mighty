# Spec: Skip Passed Bidders and Permanent Passes

## Context
During the bidding phase, the backend API currently allows players who have passed to re-enter the bidding if another player makes a bid, because the `PassedPlayers` map is cleared on every new bid. Furthermore, when advancing the turn, the logic naively increments the current turn (`(g.CurrentTurn + 1) % 5`), which causes the turn to land on players who have passed, stalling the game.

## Requirements
- Once a player passes during the bidding phase, they are permanently out of the auction.
- When advancing the turn after a bid or a pass, the game must automatically skip over any players who have already passed.
- (Existing Behavior to Preserve): If a player bids 10 (the maximum points), the bidding phase automatically resolves and that player wins the bid.

## Implementation Details
1. **Remove Pass Reset**:
   - In `internal/game/rules.go` (`ApplyMove` > `MoveBid`), remove the line `g.PassedPlayers = make(map[int]bool)`. This ensures passes are preserved throughout the bidding phase.
2. **Turn Advancement Logic**:
   - In `ApplyMove`, for both `MoveBid` (when under 10 points) and `MovePass`, replace the naive turn increment with a loop that skips passed players:
     ```go
     for {
         g.CurrentTurn = (g.CurrentTurn + 1) % 5
         if !g.PassedPlayers[g.CurrentTurn] {
             break
         }
     }
     ```
3. **Unit Tests**:
   - Add a test in `internal/game/rules_test.go` to verify that passing players are permanently skipped during subsequent turns.
   - Example flow: Player 1 bids, Players 2 and 3 pass, Player 4 bids. Assert that `CurrentTurn` becomes 5, and after Player 5 acts, it correctly loops back to Player 1 (skipping 2 and 3).

## Spec Self-Review Checklist
- [x] Placeholder scan
- [x] Internal consistency
- [x] Scope check
- [x] Ambiguity check
