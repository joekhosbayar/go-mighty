# Multi-Set Matches Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement multiple sets (rounds) of 10 tricks within the same game room, accumulating scores across sets and allowing dynamic room size changes.

**Architecture:** We add `TotalScores` and `PlayAgainVotes` to the existing backend `Game` object, along with two new moves (`play_again`, `change_config`). When votes equal `Config.NumPlayers`, the game resets its state (but keeps `TotalScores`) and starts a new round with a shifted dealer. The frontend displays the cumulative scores, voting status, and provides UI to vote or change config.

**Tech Stack:** Go (Backend), React/TypeScript (Frontend)

## Global Constraints
- Preserve all existing comments and docstrings unless explicitly told otherwise.
- Use exact file paths and names as provided.

---

### Task 1: Backend State Changes

**Files:**
- Modify: `go-mighty/internal/game/game.go`

**Interfaces:**
- Produces: `MovePlayAgain`, `MoveChangeConfig`, `Game.TotalScores`, `Game.PlayAgainVotes`

- [ ] **Step 1: Add new MoveTypes**

In `go-mighty/internal/game/game.go`, around the `MoveType` definitions:
```go
	// MovePlayAgain represents voting to play another round.
	MovePlayAgain MoveType = "play_again"
	// MoveChangeConfig represents changing the game config (e.g. NumPlayers).
	MoveChangeConfig MoveType = "change_config"

// ChangeConfigMove represents the payload for changing game config.
type ChangeConfigMove struct {
	NumPlayers int `json:"num_players"`
}
```

- [ ] **Step 2: Update Game struct**

In `type Game struct`:
```go
	// Scoring
	Scores         map[string]int `json:"scores"` // Final round scores: declarer full, revealed partner half, others 0. Card points live in Player.Points.
	TotalScores    map[string]int `json:"total_scores"` // Cumulative scores
	PlayAgainVotes map[int]bool   `json:"play_again_votes"` // Seats that voted to play again
```

- [ ] **Step 3: Initialize maps in NewWithConfig**

In `NewWithConfig`:
```go
	g := &Game{
		ID:            id,
		Status:        PhaseWaiting,
		Config:        cfg,
		Players:       [5]*Player{},
		PassedPlayers: make(map[int]bool),
		Tricks:        make([]Trick, 0),
		Scores:        make(map[string]int),
		TotalScores:   make(map[string]int),
		PlayAgainVotes: make(map[int]bool),
		Declarer:      -1,
		PartnerSeat:   -1,
		Version:       1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
```

- [ ] **Step 4: Commit**
```bash
cd go-mighty
git add internal/game/game.go
git commit -m "feat(backend): add state and moves for multi-set matches"
```

### Task 2: Backend Rules Logic

**Files:**
- Modify: `go-mighty/internal/game/rules.go`
- Modify: `go-mighty/internal/game/rules_test.go`

**Interfaces:**
- Consumes: Task 1 state changes

- [ ] **Step 1: Add score accumulation in ResolveTrick logic**

In `go-mighty/internal/game/rules.go`, locate the block where `g.Status = PhaseFinished` is set. After populating `g.Scores`, add logic to accumulate them:
```go
				if g.TotalScores == nil {
					g.TotalScores = make(map[string]int)
				}
				for pID, score := range g.Scores {
					g.TotalScores[pID] += score
				}
```

- [ ] **Step 2: Handle new moves in ApplyMove**

At the beginning of `ApplyMove`, add handling for `PhaseFinished` moves before the general switch:
```go
	if g.Status == PhaseFinished {
		if moveType == MoveChangeConfig {
			if len(payload) > 0 {
				var cm ChangeConfigMove
				if err := json.Unmarshal(payload, &cm); err == nil {
					if cm.NumPlayers == 4 || cm.NumPlayers == 5 {
						g.Config.NumPlayers = cm.NumPlayers
						g.PlayAgainVotes = make(map[int]bool) // Reset votes on config change
					}
				}
			}
			g.Version++
			g.UpdatedAt = time.Now()
			return nil
		}

		if moveType == MovePlayAgain {
			if g.PlayAgainVotes == nil {
				g.PlayAgainVotes = make(map[int]bool)
			}
			g.PlayAgainVotes[seat] = true
			
			// Check if all active seats voted
			if len(g.PlayAgainVotes) == g.Config.NumPlayers {
				g.resetForNextRound()
			}
			g.Version++
			g.UpdatedAt = time.Now()
			return nil
		}
		
		return errors.New("invalid move in finished phase")
	}
```

- [ ] **Step 3: Implement resetForNextRound**

At the bottom of `rules.go`:
```go
// resetForNextRound clears the board state and starts a new set of tricks.
func (g *Game) resetForNextRound() {
	g.PlayAgainVotes = make(map[int]bool)
	g.Tricks = make([]Trick, 0)
	g.Bids = nil
	g.CurrentBid = nil
	g.Contract = nil
	g.Declarer = -1
	g.PartnerCard = nil
	g.PartnerSeat = -1
	g.PassedPlayers = make(map[int]bool)
	g.Scores = make(map[string]int)
	g.IsNoFriend = false
	
	// Shift dealer clockwise, handling current config bounds
	g.Dealer = (g.Dealer + 1) % g.Config.NumPlayers
	
	// Clear point cards from previous round
	for i := range g.Players {
		if g.Players[i] != nil {
			g.Players[i].Points = []Card{}
		}
	}
	
	// Start re-deals cards and sets PhaseBidding
	g.Start()
	// Bidding starts with the new dealer
	g.CurrentTurn = g.Dealer
}
```

- [ ] **Step 4: Test new rules in rules_test.go**

In `go-mighty/internal/game/rules_test.go`:
```go
func TestMultiSetMatch(t *testing.T) {
	g := NewWithConfig("test", GameConfig{NumPlayers: 5})
	for i := 0; i < 5; i++ {
		g.Players[i] = &Player{ID: "p" + string(rune(i+48)), Seat: i}
	}
	g.Status = PhaseFinished
	g.Scores = map[string]int{"p0": 10, "p1": -5}
	g.TotalScores = map[string]int{"p0": 10, "p1": -5}
	g.Dealer = 0

	// Vote to play again
	for i := 0; i < 4; i++ {
		_ = g.ApplyMove("p"+string(rune(i+48)), MovePlayAgain, nil)
	}
	if g.Status != PhaseFinished {
		t.Errorf("expected game to stay in finished phase until all vote")
	}
	
	// 5th vote
	_ = g.ApplyMove("p4", MovePlayAgain, nil)
	
	if g.Status != PhaseBidding {
		t.Errorf("expected game to reset and enter bidding phase")
	}
	if g.Dealer != 1 {
		t.Errorf("expected dealer to shift to 1, got %d", g.Dealer)
	}
	if g.CurrentTurn != 1 {
		t.Errorf("expected bidding to start with dealer 1, got %d", g.CurrentTurn)
	}
	if len(g.TotalScores) == 0 {
		t.Errorf("TotalScores should persist")
	}
}
```

- [ ] **Step 5: Run tests to verify**
Run: `cd go-mighty && go test ./internal/game/... -v`
Expected: PASS

- [ ] **Step 6: Commit**
```bash
cd go-mighty
git add internal/game/rules.go internal/game/rules_test.go
git commit -m "feat(backend): implement multi-set logic and reset"
```

### Task 3: Frontend Types & State

**Files:**
- Modify: `mighty-frontend/src/core/types.ts`
- Modify: `mighty-frontend/src/core/view.ts`

- [ ] **Step 1: Update types.ts**

Update `MoveType`:
```typescript
export type MoveType = 'bid' | 'pass' | 'discard' | 'call_partner' | 'play_card' | 'play_again' | 'change_config'
```
Add `ChangeConfigPayload`:
```typescript
export interface ChangeConfigPayload {
  num_players: number
}
```
Update `Game` interface:
```typescript
  total_scores?: Record<string, number>
  play_again_votes?: Record<number, boolean>
```

- [ ] **Step 2: Update view.ts TableView and ScoreRow**

Update `ScoreRow`:
```typescript
export interface ScoreRow {
  playerId: string
  name: string
  roundScore: number
  totalScore: number
  cardPoints: number
}
```
Update `SeatView`:
```typescript
  hasVotedPlayAgain: boolean
```

- [ ] **Step 3: Compute values in view.ts tableView**

In `tableView` function, update the `seats` mapping:
```typescript
      hasVotedPlayAgain: game.play_again_votes?.[i] ?? false,
```
Update the `scores` mapping logic:
```typescript
    scores: Object.entries(game.scores ?? {}).map(([playerId, roundScore]) => ({
      playerId,
      name: game.players.find(p => p?.id === playerId)?.name ?? 'Unknown',
      roundScore,
      totalScore: game.total_scores?.[playerId] ?? roundScore,
      cardPoints: game.players.find(p => p?.id === playerId)?.points?.reduce((acc, c) => 
        acc + (c.rank === '10' || c.rank === 'J' || c.rank === 'Q' || c.rank === 'K' || c.rank === 'A' ? 1 : 0), 0) ?? 0,
    })),
```
*Note: Make sure to check the original `scores` calculation and ensure the `cardPoints` accumulator is correct based on original logic.*

- [ ] **Step 4: Verify frontend typechecks**
Run: `cd mighty-frontend && npm run build` (or check TypeScript compiler)

- [ ] **Step 5: Commit**
```bash
cd mighty-frontend
git add src/core/types.ts src/core/view.ts
git commit -m "feat(frontend): update types and view state for multi-sets"
```

### Task 4: Frontend Component Updates

**Files:**
- Modify: `mighty-frontend/src/components/ScoreBoard.tsx`
- Modify: `mighty-frontend/src/components/GameTable.tsx`
- Modify: `mighty-frontend/src/components/GameScreen.tsx`

- [ ] **Step 1: Update ScoreBoard.tsx**

Add props:
```typescript
export function ScoreBoard({ 
  view, 
  onPlayAgain, 
  onChangeConfig 
}: { 
  view: TableView
  onPlayAgain?: () => void
  onChangeConfig?: (numPlayers: number) => void
}) {
```
Add Total Score column and Play Again UI. Inside the `<tbody>`:
```tsx
              <tr key={row.playerId} data-testid={`score-row-${row.playerId}`}>
                <td style={{ fontWeight: '600' }}>
                  {row.name} {view.seats.find(s => s.name === row.name)?.hasVotedPlayAgain ? '✅' : ''}
                </td>
                <td style={{/* ... original styling ... */}}>
                  {row.roundScore > 0 ? `+${row.roundScore}` : row.roundScore}
                </td>
                <td style={{ fontFamily: 'var(--font-mono)', fontWeight: 'bold' }}>
                  {row.totalScore > 0 ? `+${row.totalScore}` : row.totalScore}
                </td>
                <td style={{ fontFamily: 'var(--font-mono)', color: 'var(--color-text-secondary)' }}>{row.cardPoints}</td>
                <td style={{ color: roleColor, fontFamily: 'var(--font-mono)', fontSize: '0.85rem', textTransform: 'uppercase' }}>{role}</td>
              </tr>
```
*(Also add the `<th>Total score</th>` in the `<thead>`)*

Below the table, add the actions:
```tsx
      <div style={{ marginTop: '2rem', display: 'flex', gap: '1rem', alignItems: 'center' }}>
        <button 
          onClick={onPlayAgain} 
          disabled={view.seats.find(s => s.isMe)?.hasVotedPlayAgain}
          style={{ padding: '0.75rem 1.5rem', background: 'var(--color-accent)', color: 'black', border: 'none', borderRadius: '4px', cursor: 'pointer', fontWeight: 'bold' }}
        >
          {view.seats.find(s => s.isMe)?.hasVotedPlayAgain ? 'Waiting for others...' : 'Play Again'}
        </button>

        <select 
          value={view.config?.num_players ?? 5} 
          onChange={(e) => onChangeConfig?.(Number(e.target.value))}
          style={{ padding: '0.75rem', borderRadius: '4px', background: 'var(--color-surface)', color: 'var(--color-text-primary)', border: '1px solid var(--color-border)' }}
        >
          <option value={4}>4-Player Game</option>
          <option value={5}>5-Player Game</option>
        </select>
      </div>
```

- [ ] **Step 2: Update GameTable.tsx**

Add the props to `GameTable` and pass them to `ScoreBoard`:
```tsx
export function GameTable({
  view,
  connection,
  error,
  onBid,
  onPass,
  onDiscard,
  onCallPartner,
  onNoFriend,
  onPlayCard,
  onLeave,
  onPlayAgain,
  onChangeConfig,
}: {
  view: TableView
  connection: 'connecting' | 'connected' | 'disconnected'
  error: string | null
  onBid: (bid: Bid) => void
  onPass: () => void
  onDiscard: (cards: Card[]) => void
  onCallPartner: (card: Card) => void
  onNoFriend: () => void
  onPlayCard: (card: Card, callJoker: boolean, calledSuit?: Suit) => void
  onLeave: () => void
  onPlayAgain?: () => void
  onChangeConfig?: (numPlayers: number) => void
}) {
  // ... inside render
  if (view.phase === 'finished') {
    return <ScoreBoard view={view} onPlayAgain={onPlayAgain} onChangeConfig={onChangeConfig} />
  }
```

- [ ] **Step 3: Update GameScreen.tsx**

Pass the actions from `GameScreen`:
```tsx
      onLeave={() => navigate('/lobby')}
      onPlayAgain={() => sendMove('play_again', null)}
      onChangeConfig={(numPlayers) => sendMove('change_config', { num_players: numPlayers })}
```

- [ ] **Step 4: Verify Frontend build**
Run: `cd mighty-frontend && npm run build`

- [ ] **Step 5: Commit**
```bash
cd mighty-frontend
git add src/components/ScoreBoard.tsx src/components/GameTable.tsx src/components/GameScreen.tsx
git commit -m "feat(frontend): add multi-set UI controls"
```
