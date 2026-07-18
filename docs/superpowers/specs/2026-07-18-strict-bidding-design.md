# Strict Bidding and Auto-Resolution Design

## Purpose
Fix a functional bug in the bidding rules where the backend was incorrectly allowing same-point bids if the suit rank was higher.

## Requirements
- A new bid must be strictly greater than the current bid's points.
- If a player submits a bid of 10 points (the maximum allowable bid), the bidding phase immediately ends and resolves, skipping any remaining passes.

## Architecture & Logic Changes

### 1. `validateBid` Update (`internal/game/rules.go`)
- **Current Behavior:** Allows `bid.Points == g.CurrentBid.Points` if the new suit is higher rank or changes to No-Trump.
- **New Behavior:** If `g.CurrentBid != nil`, explicitly require `bid.Points > g.CurrentBid.Points`. If it is less than or equal, return an `ErrInvalidMove`.
- The rule that points must be between 3 and 10 inclusive remains unchanged.

### 2. `ApplyMove` Update (`internal/game/rules.go`)
- **Current Behavior:** For a `MoveBid`, the bid is recorded, passes are cleared, and the turn advances to the next player. The phase only advances when 4 passes are accumulated.
- **New Behavior:** 
  - After successfully validating and applying a `MoveBid`, check if `bid.Points == 10`.
  - If true, instantly simulate the resolution of the bidding phase:
    - Set `g.Status = PhaseExchanging`.
    - Set `g.Contract = g.CurrentBid`.
    - Set `g.Trump = g.Contract.Suit`.
    - Assign `g.Kitty` to the Declarer's hand and clear `g.Kitty`.
    - Set `g.CurrentTurn = g.Declarer`.
  - If false, proceed as normal (advance turn to next player).

### 3. Test Updates (`internal/game/rules_test.go`)
- **Validation Tests:** Remove old tests asserting suit-rank tie-breakers. Add tests asserting that a same-point bid (e.g. 3 Spades over 3 Clubs) returns an error.
- **Resolution Tests:** Add a test verifying that when a player bids 10, the `Game.Status` immediately becomes `PhaseExchanging` and the Declarer receives the Kitty, without requiring the other 4 players to submit `MovePass`.
