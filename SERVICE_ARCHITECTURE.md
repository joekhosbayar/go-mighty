# Game Service Architecture

## Overview
The `GameService` implements the game of Mighty following the design pattern in `design.puml`. It manages:
- **Redis**: Hot game state (source of truth)
- **PostgreSQL**: Immutable ledger (audit trail)
- **Redis Pub/Sub**: Real-time event notifications

## Key Design Patterns

### Version-Based Concurrency Control
Every move includes `client_version` which must match Redis state version to prevent race conditions:
```
Client sends move with client_version=5
→ Server checks Redis version is also 5
→ If match: process move and increment to version 6
→ If mismatch: reject with stale version error
```

### Distributed Locking
Game state modifications acquire a distributed lock:
```
acquireLock(gameID)
→ checkVersion()
→ loadGameState()
→ applyMove()
→ savePostgres() // immutable ledger
→ updateRedis()  // hot state
→ publishEvent() // notify clients
→ releaseLock()
```

### Move Processing Flow
All moves follow the same pattern in `ProcessMove()`:
1. **Acquire lock** on game state
2. **Check version** - reject if stale
3. **Load state** from Redis
4. **Apply move** to game instance
5. **Insert to Postgres** - immutable record
6. **Update Redis** - new game state
7. **Publish event** - notify all clients
8. **Release lock**

## Core Operations

### CreateGame
- Creates new `game.Game` instance with 5 players
- Inserts game record to Postgres
- Initializes Redis game state with version 0
- Returns game ID and initial state

### JoinGame
- Validates game is waiting and has space
- Adds player to game
- Inserts join move to Postgres ledger
- Updates Redis state
- Publishes `player_joined` event

### StartGame
- Validates all 5 players have joined
- Starts first hand with dealer at seat 0
- Deals cards (10 per player + 3 kitty)
- Inserts start move to Postgres
- Updates game status to "in_progress"
- Publishes `game_started` event with initial cards

### ProcessMove
Generic move processor that routes to specific handlers:
- **Bid**: `applyBid()` - validates bid higher than current, updates bidding state
- **Discard**: `applyDiscard()` - declarer discards 3 cards from hand + kitty
- **Call Partner**: `applyCallPartner()` - declarer calls partner by card
- **Play Card**: `applyPlayCard()` - plays card to current trick, completes tricks, scores hands

### GetGameState
- Retrieves current game state from Redis
- Filters cards - only shows requesting player's hand
- Returns complete game state with phase, trump, scores, etc.

## Data Transfer Objects (DTOs)

### MoveRequest
```go
type MoveRequest struct {
    GameID        string          // Game identifier
    PlayerID      string          // Player making move
    ClientVersion int64           // Client's known version
    MoveType      game.MoveType   // Type of move
    Payload       json.RawMessage // Move-specific data
}
```

### GameStateSnapshot
Complete game state stored in Redis:
- GameID, Status, Variant, HandNo
- Players array (5 seats)
- CurrentHand state (phase, trump, contract, tricks)
- Magic cards (Mighty, Joker, Ripper)
- Version (for optimistic locking)
- UpdatedAt timestamp

### GameEvent
Published to Redis pub/sub channel:
```go
type GameEvent struct {
    GameID    string          // Game identifier
    Type      string          // Event type (player_joined, card_played, etc.)
    Timestamp time.Time       // Event time
    Version   int64           // Game version after event
    Payload   json.RawMessage // Event-specific data
}
```

## Move Payloads

### BidPayload
```json
{
  "points": 15,
  "trump": "spades",
  "no_trump": false,
  "no_friend": false
}
```

### DiscardPayload
```json
{
  "discarded_cards": ["H3", "D4", "C5"]
}
```

### CallPartnerPayload
```json
{
  "card": "SA",
  "suit": "spades",
  "rank": "A"
}
```

### PlayCardPayload
```json
{
  "card": "HA"
}
```

## Redis Keys

### Game State
- Key: `game:{gameID}:state`
- Type: String (JSON)
- TTL: 24 hours
- Content: Full `GameStateSnapshot`

### Version Counter
- Key: `game:{gameID}:version`
- Type: String (int64)
- TTL: 24 hours
- Content: Current version number

### Lock
- Key: `game:{gameID}:lock`
- Type: String
- TTL: 5 seconds
- Content: "locked"

### Pub/Sub Channel
- Channel: `game:{gameID}:events`
- Content: JSON encoded `GameEvent`

## PostgreSQL Tables

### games
Primary game records with metadata:
- id, created_at, variant, status, max_players, version

### hands
Individual hand/deal records:
- id, game_id, hand_no, dealer_seat, status, created_at

### moves
Immutable ledger of all moves:
- id, game_id, player_id, seat_no, version, client_version, move_type, payload_json, created_at

### hand_results
Final scoring for completed hands:
- hand_id, game_id, declarer_seat, partner_seat, bid_points, points_taken, success, score_s, multipliers

### hand_player_scores
Individual player scores per hand:
- hand_id, game_id, seat_no, player_id, role, raw_s, final_score

## Error Handling

### Version Mismatch
```go
return nil, fmt.Errorf("version mismatch: client=%d, server=%d", req.ClientVersion, currentVersion)
```
Client must refresh state and retry.

### Invalid Move
Game logic validation errors (e.g., `ErrNotYourTurn`, `ErrInvalidCard`) are returned directly.
Client must fix move before retrying.

### Lock Timeout
If lock acquisition fails, return timeout error.
Client should retry after brief delay.

## Mighty Game Rules Implementation

### Bidding Phase
- Players bid 13-20 points in clockwise order
- Must bid higher than current bid
- No-trump beats same-point suit bid
- Last bidder becomes declarer

### Discard Phase
- Declarer picks up 3 kitty cards (13 total)
- Must discard back to 10 cards
- Discarded cards count toward declarer's points

### Partner Call Phase
- Declarer calls partner by specific card
- Partner identity revealed when called card is played
- No-friend option: declarer plays alone

### Play Phase
- 10 tricks of 5 cards each
- Must follow suit if possible
- Trump beats off-suit, higher rank beats lower
- Special cards: Mighty > Joker > Trump
- Ripper (C3/S3) can force Joker to be played

### Scoring
```
Base Score (S) = 2×(B - 13) + (P - B)
Where:
  B = bid points
  P = points taken by declarer team
```

Multipliers (cumulative):
- Run (20 points): ×2
- Back-run (≥11 defender points): ×2
- No-trump: ×2
- No-friend: ×2

Example: 15-bid no-trump run = 4 × [2×(15-13) + (20-15)] = 4 × 9 = 36 points

## Integration with HTTP Handlers

The service should be used by HTTP handlers in `internal/api/router/router.go`:

```go
func (h *Handler) CreateGame(w http.ResponseWriter, r *http.Request) {
    gameID, snapshot, err := h.gameService.CreateGame(r.Context(), "standard")
    // ... handle response
}

func (h *Handler) SubmitMove(w http.ResponseWriter, r *http.Request) {
    var req service.MoveRequest
    json.NewDecoder(r.Body).Decode(&req)
    
    response, err := h.gameService.ProcessMove(r.Context(), req)
    // ... handle response
}
```

## Future Enhancements

1. **WebSocket Support**: Replace polling with WebSocket for real-time event streaming
2. **Replay System**: Use moves table to replay games from any point
3. **AI Players**: Integrate bot players for testing/single-player
4. **Leaderboard**: Track player statistics across games
5. **Tournament Mode**: Multi-game tournaments with brackets
6. **Spectator Mode**: Allow observers to watch games in progress

## Testing Strategy

### Unit Tests
Test individual move handlers with mock Redis/Postgres:
- Valid moves succeed and update state correctly
- Invalid moves return appropriate errors
- Version conflicts are detected
- Edge cases (Mighty, Joker, Ripper) work correctly

### Integration Tests
Test full flow with real Redis/Postgres:
- Complete game from start to finish
- Concurrent moves are handled safely
- Event publication works correctly
- Database consistency maintained

### Load Tests
Simulate multiple concurrent games:
- Lock contention handling
- Redis connection pooling
- Database connection pooling
- Event delivery latency
