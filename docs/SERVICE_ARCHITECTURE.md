# Game Service Architecture

## Overview
The `GameService` implements the game of Mighty. It manages:
- **Redis**: Hot game state (source of truth).
- **PostgreSQL**: Immutable ledger (audit trail) and persistent User Profiles.
- **Redis Pub/Sub**: Real-time event notifications for high-performance reactive updates.

## Key Design Patterns

### Version-Based Concurrency Control (Optimistic Locking)
Every move includes `client_version` which must match the current server-side version to prevent race conditions during simultaneous plays.

### Distributed Locking
Game state modifications are protected by a Redis-based distributed lock to ensure atomicity during complex state transitions (like dealing or resolving tricks).

### Structure-Agnostic Unmarshaling
The API layer implements a robust unmarshaling strategy that supports both legacy raw card payloads and the new nested `PlayCardMove` objects, ensuring compatibility across different client implementations.

## Core Operations

### CreateGame
- Generates a short (8-digit) authoritative game ID.
- Requires JWT Authentication.
- Automatically joins the creator at **Seat 0**.
- Initializes hot state in Redis and audit record in Postgres.

### JoinGame
- Idempotent: If a player is already in the requested seat, it returns success.
- Validates game status and seat availability.
- Triggers **Game Start** and automatic **Dealing** when the 5th player joins.

### ProcessMove
Unified entry point for all game actions:
- **Bid / Pass**: Manages the bidding rotation until 4 consecutive passes.
- **Discard**: Allows the declarer to swap cards with the kitty.
- **Call Partner**: Sets the secret partner card.
- **Play Card**: Executes trick resolution, power calculations, and rule enforcement.

## Data Structures

### Game State (Redis)
Stored as JSON with the following key fields:
- `id`: Short authoritative ID.
- `status`: current `Phase` (waiting, bidding, exchanging, calling, playing, finished).
- `version`: Monotonic counter for concurrency control.
- `declarer`: Seat index of the contract winner.
- `trump`: Current trump suit (if any).

### User Identity (Postgres)
- `users`: ID, Username, PasswordHash, Email.
- `user_stats`: Persistent tracking of total games, wins, and UCLA points.

## Real-Time Layer (WebSockets)
- **Bi-Directional**: Supports both state broadcasts (Outbound) and game moves (Inbound).
- **Handshake Authentication**: Requires a JWT token in the query string (`?token=...`).
- **Heartbeat**: 30-second ping/pong cycle to manage connection health.

## Special Logic Enforcement

### Contextual Power Matrix
Card strength is not static. It is calculated dynamically based on:
- Trick number (Joker loses power on Trick 1 and 10).
- Presence of the Mighty.
- Joker Caller "Ripper" leads.

### First-Trick Restrictions
- No trump leads on Trick 1 unless holding only trumps.
- No Mighty play on Trick 1 unless the player cannot follow the lead suit.
