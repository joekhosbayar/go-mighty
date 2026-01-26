# Mighty Card Game API Documentation

## Overview
RESTful API for the Mighty multiplayer card game backend. All endpoints require JWT authentication via `Authorization: Bearer <token>` header. For testing, use `X-Player-ID` header.

**Base URL**: `http://localhost:8080`

## Authentication
- **Production**: JWT token in `Authorization: Bearer <token>` header
- **Testing**: `X-Player-ID` header with player identifier

## Response Format
All responses are JSON with appropriate HTTP status codes:
- `2xx`: Success
- `4xx`: Client error (bad request, unauthorized, etc.)
- `5xx`: Server error

### Error Response
```json
{
  "error": "Error message description"
}
```

---

## Game Management Endpoints

### Create Game
Creates a new game session.

**Endpoint**: `POST /games`

**Request Body**:
```json
{
  "game_id": "game-123",        // Optional, auto-generated if omitted
  "max_players": 5              // Optional, defaults to 5
}
```

**Response** (`201 Created`):
```json
{
  "game_id": "game-123",
  "status": "waiting",
  "max_players": 5,
  "variant": "standard"
}
```

**Example**:
```bash
curl -X POST http://localhost:8080/games \
  -H "Content-Type: application/json" \
  -d '{"max_players": 5}'
```

---

### Join Game
Allows a player to join an existing game.

**Endpoint**: `POST /games/{gameId}/join`

**Headers**:
- `X-Player-ID`: Player identifier (required)

**Request Body**:
```json
{
  "seat_no": 0   // Seat number (0-4)
}
```

**Response** (`200 OK`):
```json
{
  "success": true,
  "message": "Successfully joined game",
  "seat_no": 0
}
```

**Errors**:
- `401`: Missing player ID
- `400`: Invalid seat number or seat already taken

**Example**:
```bash
curl -X POST http://localhost:8080/games/game-123/join \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player1" \
  -d '{"seat_no": 0}'
```

---

### Start Game
Starts the game after all 5 players have joined.

**Endpoint**: `POST /games/{gameId}/start`

**Headers**:
- `X-Player-ID`: Player identifier (required)

**Response** (`200 OK`):
```json
{
  "success": true,
  "message": "Game started successfully"
}
```

**Errors**:
- `401`: Missing player ID
- `400`: Not all players joined yet

**Example**:
```bash
curl -X POST http://localhost:8080/games/game-123/start \
  -H "X-Player-ID: player1"
```

---

### Get Game State
Retrieves the current game state. Only shows the requesting player's cards.

**Endpoint**: `GET /games/{gameId}/state`

**Headers**:
- `X-Player-ID`: Player identifier (required)

**Response** (`200 OK`):
```json
{
  "game_id": "game-123",
  "status": "in_progress",
  "variant": "standard",
  "hand_no": 1,
  "version": 15,
  "players": [
    {
      "player_id": "player1",
      "seat_no": 0,
      "score": 0
    },
    // ... 4 more players
  ],
  "current_hand": {
    "hand_no": 1,
    "phase": "bidding",
    "dealer_seat": 0,
    "current_bidder": 1,
    "contract": null,
    "declarer_seat": -1,
    "partner_seat": -1,
    "cards_in_hand": {
      "0": ["SA", "SK", "HQ", "DJ", "C10", "H9", "D8", "S7", "H6", "C5"]
    },
    "current_trick": null,
    "trick_no": 0,
    "leader_seat": -1
  },
  "magic_cards": {
    "mighty": "SA",
    "joker": "JK",
    "ripper": "C3"
  },
  "updated_at": "2026-01-02T10:30:00Z"
}
```

**Game Phases**:
- `dealing`: Cards being dealt
- `bidding`: Players bidding for contract
- `kitty_exchange`: Declarer picking up kitty and discarding
- `partner_call`: Declarer calling partner
- `playing`: Playing tricks
- `scoring`: Calculating scores
- `complete`: Hand finished

**Example**:
```bash
curl http://localhost:8080/games/game-123/state \
  -H "X-Player-ID: player1"
```

---

### Submit Move
Submits a game move (bid, discard, call partner, play card).

**Endpoint**: `POST /games/{gameId}/moves`

**Headers**:
- `X-Player-ID`: Player identifier (required)

**Request Body**:
```json
{
  "client_version": 15,
  "move_type": "bid",
  "payload": {
    // Move-specific payload (see below)
  }
}
```

**Response** (`200 OK`):
```json
{
  "success": true,
  "server_version": 16,
  "event": {
    "game_id": "game-123",
    "type": "bid_placed",
    "timestamp": "2026-01-02T10:31:00Z",
    "version": 16,
    "payload": {
      "player_id": "player1",
      "seat_no": 0,
      "bid": {
        "points": 15,
        "trump": "spades",
        "no_trump": false,
        "no_friend": false
      }
    }
  }
}
```

**Errors**:
- `401`: Missing player ID
- `400`: Invalid move or not your turn
- `409`: Version mismatch (client needs to refresh state)

---

## Move Types

### 1. Bid
Place a bid during bidding phase.

**Move Type**: `"bid"`

**Payload**:
```json
{
  "points": 15,              // 13-20
  "trump": "spades",         // "spades", "hearts", "diamonds", "clubs"
  "no_trump": false,         // true for no-trump bid
  "no_friend": false         // true to play alone
}
```

**Rules**:
- Must bid higher than current bid
- No-trump beats same-point suit bid
- Minimum bid: 13 points
- Maximum bid: 20 points

**Example**:
```bash
curl -X POST http://localhost:8080/games/game-123/moves \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player1" \
  -d '{
    "client_version": 15,
    "move_type": "bid",
    "payload": {
      "points": 15,
      "trump": "spades",
      "no_trump": false,
      "no_friend": false
    }
  }'
```

---

### 2. Pass Bid
Pass on bidding.

**Move Type**: `"bid_pass"`

**Payload**: `null` or `{}`

**Example**:
```bash
curl -X POST http://localhost:8080/games/game-123/moves \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player2" \
  -d '{
    "client_version": 16,
    "move_type": "bid_pass",
    "payload": {}
  }'
```

---

### 3. Discard
Declarer discards 3 cards after picking up kitty.

**Move Type**: `"discard"`

**Payload**:
```json
{
  "discarded_cards": ["H3", "D4", "C5"]
}
```

**Rules**:
- Must discard exactly 3 cards
- Discarded cards count toward declarer's points
- Can discard any cards from 13-card hand (10 dealt + 3 kitty)

**Example**:
```bash
curl -X POST http://localhost:8080/games/game-123/moves \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player1" \
  -d '{
    "client_version": 20,
    "move_type": "discard",
    "payload": {
      "discarded_cards": ["H3", "D4", "C5"]
    }
  }'
```

---

### 4. Call Partner
Declarer calls partner by specifying a card.

**Move Type**: `"call_partner"`

**Payload**:
```json
{
  "card": "SA",
  "suit": "spades",
  "rank": "A"
}
```

**Rules**:
- Partner is revealed when the called card is played
- Cannot call a card in declarer's hand
- Can call "no friend" to play alone (specified in bid)

**Example**:
```bash
curl -X POST http://localhost:8080/games/game-123/moves \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player1" \
  -d '{
    "client_version": 21,
    "move_type": "call_partner",
    "payload": {
      "card": "SA",
      "suit": "spades",
      "rank": "A"
    }
  }'
```

---

### 5. Play Card
Play a card to the current trick.

**Move Type**: `"play_card"`

**Payload**:
```json
{
  "card": "HA"
}
```

**Rules**:
- Must follow suit if possible
- Trump beats off-suit
- Special cards: Mighty > Joker > Trump > Lead suit
- Ripper (C3/S3) can force Joker to be played

**Example**:
```bash
curl -X POST http://localhost:8080/games/game-123/moves \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player1" \
  -d '{
    "client_version": 22,
    "move_type": "play_card",
    "payload": {
      "card": "HA"
    }
  }'
```

---

## Card Notation

### Standard Cards
Format: `<Suit><Rank>`
- Suits: `S` (Spades), `H` (Hearts), `D` (Diamonds), `C` (Clubs)
- Ranks: `A`, `K`, `Q`, `J`, `10`, `9`, `8`, `7`, `6`, `5`, `4`, `3`, `2`

Examples: `SA` (Ace of Spades), `H10` (10 of Hearts), `D3` (3 of Diamonds)

### Special Cards
- **Joker**: `JK`
- **Mighty**: Ace of trump suit (or DA if no-trump)
- **Ripper**: 3 of trump suit (or C3 if no-trump)

---

## Game Events (Pub/Sub)

Clients can subscribe to Redis pub/sub channel: `game:{gameId}:events`

### Event Types
- `player_joined`: Player joined the game
- `game_started`: Game started, cards dealt
- `bid_placed`: Player placed a bid
- `bid_passed`: Player passed on bidding
- `contract_won`: Final bid determined, declarer set
- `cards_discarded`: Declarer discarded cards
- `partner_called`: Partner card called
- `card_played`: Card played to trick
- `trick_completed`: Trick finished, winner determined
- `partner_revealed`: Partner identity revealed
- `hand_completed`: Hand finished, scores calculated
- `game_ended`: Game over

### Event Format
```json
{
  "game_id": "game-123",
  "type": "card_played",
  "timestamp": "2026-01-02T10:35:00Z",
  "version": 25,
  "payload": {
    "player_id": "player2",
    "seat_no": 1,
    "card": "HK"
  }
}
```

---

## Scoring

### Mighty Scoring Formula
```
Base Score (S) = 2 × (B - 13) + (P - B)

Where:
  B = Bid points (13-20)
  P = Points taken by declarer team
```

### Multipliers (Cumulative)
- **Run** (20 points): ×2
- **Back-run** (≥11 defender points): ×2
- **No-trump**: ×2
- **No-friend**: ×2

### Example
15-bid, no-trump, run (20 points taken):
```
S = 2 × (15 - 13) + (20 - 15) = 4 + 5 = 9
Multiplier = 2 (no-trump) × 2 (run) = 4
Final Score = 9 × 4 = 36 points
```

### Distribution
- **Declarer**: +S × multiplier
- **Partner**: +S × multiplier
- **Opponents**: -S × multiplier (split 3 ways)

---

## Version Control

Every move includes `client_version` to prevent race conditions:

1. Client loads game state with version N
2. Client submits move with `client_version: N`
3. Server checks current version is also N
4. If match: process move, increment to N+1
5. If mismatch: return 409 Conflict

**Handling Version Conflicts**:
```bash
# Client gets 409 response
{
  "error": "version mismatch: client=15, server=17"
}

# Client must:
1. Fetch latest game state (GET /games/{gameId}/state)
2. Re-validate move with new state
3. Re-submit with updated client_version
```

---

## Complete Game Flow Example

```bash
# 1. Create game
curl -X POST http://localhost:8080/games \
  -H "Content-Type: application/json" \
  -d '{"max_players": 5}'
# Response: {"game_id": "game-123", ...}

# 2. All 5 players join
for i in {0..4}; do
  curl -X POST http://localhost:8080/games/game-123/join \
    -H "Content-Type: application/json" \
    -H "X-Player-ID: player$i" \
    -d "{\"seat_no\": $i}"
done

# 3. Start game
curl -X POST http://localhost:8080/games/game-123/start \
  -H "X-Player-ID: player0"

# 4. Get initial state
curl http://localhost:8080/games/game-123/state \
  -H "X-Player-ID: player0"
# Response: {"version": 5, "current_hand": {"phase": "bidding", ...}}

# 5. Bidding phase
curl -X POST http://localhost:8080/games/game-123/moves \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player1" \
  -d '{
    "client_version": 5,
    "move_type": "bid",
    "payload": {"points": 13, "trump": "spades", "no_trump": false, "no_friend": false}
  }'

# 6. Other players bid or pass...

# 7. Winner discards
curl -X POST http://localhost:8080/games/game-123/moves \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player1" \
  -d '{
    "client_version": 10,
    "move_type": "discard",
    "payload": {"discarded_cards": ["H3", "D4", "C5"]}
  }'

# 8. Call partner
curl -X POST http://localhost:8080/games/game-123/moves \
  -H "Content-Type: application/json" \
  -H "X-Player-ID: player1" \
  -d '{
    "client_version": 11,
    "move_type": "call_partner",
    "payload": {"card": "SA", "suit": "spades", "rank": "A"}
  }'

# 9. Play 10 tricks (50 cards total)...

# 10. Hand completes, scores calculated automatically
# 11. New hand starts automatically
```

---

## Testing Endpoints

### Not Yet Implemented
The following endpoints return `501 Not Implemented`:
- `GET /games` - List games
- `GET /games/{gameId}` - Get game metadata
- `PATCH /games/{gameId}` - Update game settings
- `DELETE /games/{gameId}` - Delete game
- `POST /games/{gameId}/leave` - Leave game
- `GET /games/{gameId}/players` - List players
- `PATCH /games/{gameId}/players/{playerId}` - Update player
- `GET /games/{gameId}/moves` - Get move history
- `GET /games/{gameId}/score` - Get current scores
- `GET /players/{playerId}/history` - Player history

---

## Error Codes

| Status | Meaning | Common Causes |
|--------|---------|---------------|
| 200 | OK | Request successful |
| 201 | Created | Game created successfully |
| 400 | Bad Request | Invalid move, not your turn, invalid payload |
| 401 | Unauthorized | Missing or invalid authentication |
| 404 | Not Found | Game not found |
| 409 | Conflict | Version mismatch, need to refresh state |
| 500 | Internal Server Error | Server-side error |
| 501 | Not Implemented | Endpoint not yet implemented |

---

## Rate Limiting
Currently no rate limiting implemented. In production, consider:
- Per-IP rate limits
- Per-player move frequency limits
- Connection-based throttling for pub/sub

---

## WebSocket Support (Future)
Currently using HTTP polling + Redis pub/sub. Future versions will support:
- WebSocket connections for real-time updates
- Server-Sent Events (SSE) for event streaming
- Reduced latency and bandwidth usage

---

## Development Notes

### Testing Authentication
Use `X-Player-ID` header for local testing:
```bash
-H "X-Player-ID: test-player-123"
```

### Production Authentication
Implement JWT extraction in `getPlayerIDFromAuth()`:
```go
// Extract player_id from JWT claims
claims := extractJWTClaims(r.Header.Get("Authorization"))
return claims["player_id"]
```

### Redis Configuration
Default: `localhost:6379`, DB 0, no password

Update in `cmd/mighty/main.go` for production:
```go
pubsubClient := redis.NewClient(&redis.Options{
    Addr:     os.Getenv("REDIS_URL"),
    Password: os.Getenv("REDIS_PASSWORD"),
    DB:       0,
})
```

### Database Migrations
Run SQL schema from schema design documentation before first use.

---

## Support
For issues or questions, see the codebase documentation:
- `SERVICE_ARCHITECTURE.md` - Service layer design
- `rules.md` - Mighty game rules
- `design.puml` - System architecture diagram
