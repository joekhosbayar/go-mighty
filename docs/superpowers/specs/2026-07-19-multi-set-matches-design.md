# Multi-Set Matches & Cumulative Scoring Design

## Overview
Allows players to play multiple rounds (sets of 10 tricks) within the same game room, accumulating scores across sets. This ensures players can continuously play without returning to the lobby and manually creating a new game.

## Data Structure Changes (`internal/game/game.go`)
- **`TotalScores map[string]int`**: Added to the `Game` struct to track cumulative scores for each player ID across all sets.
- **`PlayAgainVotes map[int]bool`**: Added to the `Game` struct to track which seats have voted to play another set.
- **`MoveType` additions**:
  - `MovePlayAgain`: Action sent by a player clicking "Play Again".
  - `MoveChangeConfig`: Action sent to change the room configuration (specifically, the number of players).

## Backend Logic & Data Flow (`internal/game/rules.go`)
- **Score Accumulation**: When a round ends and the game transitions to `PhaseFinished`, the calculated `Scores` for the current round are added to the cumulative `TotalScores`.
- **Voting Mechanism**: During `PhaseFinished`, any player can submit a `MovePlayAgain`. Their vote is recorded in `PlayAgainVotes` using their seat index.
- **Dynamic Player Counts**: During `PhaseFinished`, players can submit a `MoveChangeConfig` to change `g.Config.NumPlayers` (e.g., from 5 down to 4 if someone leaves). Changing the configuration clears all current `PlayAgainVotes` to ensure everyone re-consents to the new game size.
- **Round Reset**: Once the number of "Play Again" votes equals the current `g.Config.NumPlayers` (e.g., 5 votes for a 5-player game, or 4 votes for a 4-player game):
  - Retain: `TotalScores`, `ID`, `Config`, `Players`.
  - Reset: `PlayAgainVotes`, `Tricks`, `Bids`, `CurrentBid`, `Declarer`, `PartnerCard`, `PartnerSeat`, `PassedPlayers`, and current round `Scores`.
  - Shift `Dealer` clockwise: `(Dealer + 1) % g.Config.NumPlayers`.
  - Deal new hands and advance to `PhaseBidding` (similar logic to `g.Start()`, but honoring the new `Dealer` position for turn order).

## Frontend Changes
- **Game Finished Screen**: 
  - Show a "Play Again" button for the user to submit their vote.
  - Display the current voting status (e.g., "3/5 players ready" and checkmarks next to player names).
  - Add a UI control to switch the room size between 4 and 5 players dynamically.
- **Score Display**:
  - Show two columns/sections for scores on the finished screen: 'Round Score' (from the set that just finished) and 'Total Score' (cumulative across all sets).
- **Lobby/Routing**:
  - If a player leaves during the finished state, they are taken back to the lobby. Their seat in the game opens up, and any existing votes are cleared. The remaining players can either wait for a new player to fill the seat or change the config to a 4-player game to proceed immediately.
