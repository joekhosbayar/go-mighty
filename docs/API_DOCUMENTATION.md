# Mighty Card Game API Documentation

## Overview
RESTful and WebSocket API for the Mighty multiplayer card game. All state-changing endpoints require JWT authentication.

**Base URL**: `http://localhost:8080`

## Authentication
Identity is managed via JSON Web Tokens (JWT). 
1. **Signup**: Register at `/auth/signup` to receive a permanent UUID.
2. **Login**: Authenticate at `/auth/login` to receive a JWT.
3. **Usage**: 
   - **REST**: Attach `Authorization: Bearer <token>` header.
   - **WebSockets**: Pass `?token=<token>` as a query parameter.

---

## Auth Endpoints

### Signup
**Endpoint**: `POST /auth/signup`
**Payload**: `{"username": "...", "password": "...", "email": "..."}`
**Status**: `201 Created`

### Login
**Endpoint**: `POST /auth/login`
**Payload**: `{"username": "...", "password": "..."}`
**Response**: `{"token": "..."}`

---

## Game Management Endpoints

### Create Game
Creates a new game and automatically joins the creator at Seat 0.

**Endpoint**: `POST /games`
**Authentication**: Required (Bearer Token)
**Response** (`200 OK`): Full `Game` object with a server-generated short ID.

---

### Join Game
Allows a player to take an available seat.

**Endpoint**: `POST /games/{id}/join`
**Authentication**: Required (Bearer Token)
**Request Body**:
```json
{
  "seat": 1   // Seat number (0-4)
}
```
**Notes**: Game automatically starts and deals once the 5th player joins.

---

### List Lobby
List games looking for players.

**Endpoint**: `GET /games?status=waiting`

---

### Get Game State
Retrieves the current full state of the game.

**Endpoint**: `GET /games/{id}`
**Authentication**: Not strictly required for state view, but JWT is recommended for filtered views.

---

### Submit Move (REST)
Submits a game action. Recommended only for slow-turn actions or as a WebSocket fallback.

**Endpoint**: `POST /games/{id}/move`
**Request Body**:
```json
{
  "player_id": "uuid-here",
  "move_type": "bid | pass | discard | call_partner | play_card",
  "client_version": 15,
  "payload": { ... } // Move-specific payload
}
```

---

## WebSocket Interface
The primary interface for real-time Mighty gameplay. Supports bi-directional actions.

**Endpoint**: `GET /games/{id}/ws?token=<jwt>`

### Outbound Events
The server broadcasts the full `Game` JSON object to all connected clients whenever any state change occurs.

### Inbound Actions
Clients can send moves directly over the socket:
```json
{
  "type": "MOVE",
  "move_type": "play_card",
  "client_version": 5,
  "payload": {
    "card": { "suit": "spades", "rank": "A" },
    "call_joker": false
  }
}
```

---

## Move Payloads

### 1. Bid / Pass
- **bid**: `{"suit": "spades", "points": 7}` (Points: 3-10)
- **pass**: `null`

### 2. Discard
`[{"suit": "hearts", "rank": "2"}, ...]` (Exactly 3 cards)

### 3. Call Partner
`{"suit": "hearts", "rank": "A"}`

### 4. Play Card
```json
{
  "card": {"suit": "clubs", "rank": "10"},
  "call_joker": false // Set to true if leading the Joker Caller (3-Clubs)
}
```

---

## Special Card Identities
- **Mighty**: ♠A (Shifts to ♣A if Spades are trump).
- **Joker**: Wins all tricks except Trick 1, Trick 10, or when the Mighty is present.
- **Joker Caller**: ♣3 (Shifts to ♠3 if Clubs are trump).

---

## Scoring (UCLA Campus Rules)
- **Winning Score**: `(Contract * 10) + (OverTricks * 5)`
- **Losing Score**: `-(Contract * 10)` (with additional penalties if down > 1).
- **Multipliers**: Stacking `x2` for **No-Trump**, **No-Friend**, and **10-Bids**.
- **Cap**: Maximum score/loss is capped at **800** points.
