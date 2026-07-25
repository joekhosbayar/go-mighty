# Lobby WebSocket Integration Design

## 1. Goal
Transition the frontend lobby page (`LobbyScreen.tsx`) from a HTTP long-polling architecture (fetching `GET /games` every 3 seconds) to a real-time WebSocket connection using incremental updates. This minimizes bandwidth and eliminates polling-induced rate-limit exhaustion for users behind NATs.

## 2. Architecture

The system will use a dedicated WebSocket endpoint (`GET /lobby/ws`) to push incremental game state changes to the frontend via Redis Pub/Sub.

### 2.1 Backend: Event Publishing
- **Component:** `internal/service/game.go`
- **Action:** During `CreateGame` and `JoinGame`, the service will publish a JSON payload to a Redis Pub/Sub channel named `lobby_events`.
- **Payload Format:**
  - Game Creation: `{ "type": "game_created", "game": <Game Object> }`
  - Game Joined: `{ "type": "game_joined", "game_id": "string", "players_seated": number, "max_players": number }`

### 2.2 Backend: Lobby WebSocket Endpoint
- **Component:** `internal/api/lobby_ws.go` (new file) & `cmd/server/main.go`
- **Endpoint:** `GET /lobby/ws`
- **Safeguards:** Handled identically to the existing game WebSocket. Wrapped in `RequireAuth` and the standard connection limits.
- **Behavior:**
  1. Upgrades HTTP to WebSocket.
  2. Subscribes to the `lobby_events` Redis channel.
  3. Spins up a goroutine that reads messages from the channel and pushes them to the client.
  4. Automatically un-subscribes and closes cleanly upon client disconnect.

### 2.3 Frontend: WebSocket Management Hook
- **Component:** `src/hooks/useLobbyWebSocket.ts` (new file)
- **Behavior:**
  - Manages the WebSocket lifecycle.
  - Implements basic auto-reconnect logic (e.g., 3s retry) if the connection drops unexpectedly.
  - Exposes an event listener/callback for the UI to consume incoming messages.

### 2.4 Frontend: Lobby UI Integration
- **Component:** `src/components/LobbyScreen.tsx`
- **Behavior:**
  - On initial mount, calls `GET /games` to fetch the baseline open tables and populates the `games` state array.
  - Replaces the `setInterval` polling with the `useLobbyWebSocket` hook.
  - Processes incoming WS events to incrementally merge changes into the `games` state:
    - On `game_created`: Prepend the new game to the list.
    - On `game_joined`: Update the seated count. If the table is full, optionally remove it from the open tables view.

## 3. Out of Scope
- Multiplexing the lobby events into the existing game WebSocket.
- Converting all frontend network calls to WebSockets (game creation and joining remain standard HTTP POST requests).
