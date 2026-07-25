# Lobby WebSocket Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transition the frontend lobby page from HTTP long-polling to a real-time WebSocket connection using incremental updates to minimize bandwidth and eliminate rate-limit exhaustion.

**Architecture:** A new dedicated WebSocket endpoint (`GET /lobby/ws`) will push incremental game state changes (creation, joins) to the frontend via Redis Pub/Sub, while the frontend replaces its `setInterval` polling with a custom WebSocket hook.

**Tech Stack:** Go 1.25, `gorilla/websocket`, React, TypeScript, AWS Amplify Auth

## Global Constraints

- Region: **us-east-1** for every AWS resource and CLI call.
- Compute: **t4g.small (ARM64/Graviton)** — every image must be built and pushed as **linux/arm64**.
- Domain: **`themighty.gg`**. API is `api.themighty.gg`. The SPA is served by Amplify at the **apex and `www`**.
- Rate limiting **fails open**.
- All Go local paths are relative to the `go-mighty` repo root. Run `gofmt -l .` and `go vet ./...` before every commit.
- All Frontend local paths are relative to the `mighty-frontend` repo root.

---

### Task 1: Backend - Publish Lobby Events

**Files:**
- Modify: `internal/service/game_service.go:73-85`
- Modify: `internal/service/game_service.go:160-170`

**Interfaces:**
- Consumes: `RedisStore.PublishEvent`
- Produces: Redis Pub/Sub events on the `lobby_events` channel.

- [ ] **Step 1: Publish game creation event**

In `internal/service/game_service.go`, after `s.redisStore.SaveGame` inside `CreateGame`, add the publish call:

```go
	// Save to Redis (hot state)
	if err := s.redisStore.SaveGame(ctx, g, 0); err != nil {
		return nil, fmt.Errorf("failed to save game in redis: %w", err)
	}

	_ = s.redisStore.PublishEvent(ctx, "lobby_events", map[string]any{
		"type": "game_created",
		"game": g,
	})

	return g, nil
```

- [ ] **Step 2: Publish game joined event**

In `internal/service/game_service.go`, inside `JoinGame`, replace the existing `PublishEvent` call with a dual publish (one to the game's channel, one to the lobby channel):

```go
	// Publish to game channel
	_ = s.redisStore.PublishEvent(ctx, gameID, map[string]any{
		"type":    "player_joined",
		"player":  g.Players[seat],
		"version": g.Version,
	})

	// Publish to lobby channel
	_ = s.redisStore.PublishEvent(ctx, "lobby_events", map[string]any{
		"type":           "game_joined",
		"game_id":        gameID,
		"players_seated": len(g.Players) - g.EmptySeats(), // Note: you may need a helper if EmptySeats isn't available, or just manually count non-nil players. Let's use a manual count:
	})
```
*Wait, let's write a robust version of Step 2:*

```go
	// Publish to game channel
	_ = s.redisStore.PublishEvent(ctx, gameID, map[string]any{
		"type":    "player_joined",
		"player":  g.Players[seat],
		"version": g.Version,
	})

	seated := 0
	for _, p := range g.Players {
		if p != nil {
			seated++
		}
	}

	// Publish to lobby channel
	_ = s.redisStore.PublishEvent(ctx, "lobby_events", map[string]any{
		"type":           "game_joined",
		"game_id":        gameID,
		"players_seated": seated,
		"max_players":    g.NumSeatsPublic(),
	})
```

- [ ] **Step 3: Run Go tests**

Run: `go test ./internal/service/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./internal/service/
git add internal/service/game_service.go
git commit -m "feat(service): publish lobby events on game create and join"
```

---

### Task 2: Backend - Lobby WebSocket Endpoint

**Files:**
- Create: `internal/api/lobby_ws.go`
- Modify: `cmd/server/main.go:170-175`

**Interfaces:**
- Consumes: `Handler`, `upgrader()`, `sendWSError`, `closeWithCode`, `ClientIP`
- Produces: `GET /lobby/ws` endpoint.

- [ ] **Step 1: Write the handler implementation**

Create `internal/api/lobby_ws.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/rs/zerolog/log"
)

// LobbyWSHandler handles websocket connections for the global lobby feed.
func (h *Handler) LobbyWSHandler(w http.ResponseWriter, r *http.Request) {
	up := h.upgrader()

	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to upgrade lobby websocket")
		return
	}
	defer func() { _ = conn.Close() }()

	conn.SetReadLimit(maxWSMessageBytes)

	var wsWriteMu sync.Mutex
	sendError := func(errMsg string) {
		if wsErr := h.sendWSError(conn, errMsg, &wsWriteMu); wsErr != nil {
			log.Warn().Err(wsErr).Msg("Failed to send lobby websocket error")
		}
	}

	// 1. Wait for First Message Auth with 5s timeout
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	_, authMessage, err := conn.ReadMessage()
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			sendError("auth timed out")
		} else {
			sendError("failed to read auth message")
		}
		return
	}

	var authReq struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(authMessage, &authReq); err != nil || authReq.Type != "AUTH" {
		sendError("expected AUTH message")
		return
	}

	claims, err := h.authSvc.ValidateToken(r.Context(), authReq.Token)
	if err != nil {
		if errors.Is(err, service.ErrInvalidToken) {
			sendError("unauthorized")
		} else {
			sendError("auth unavailable")
		}
		return
	}

	if h.conns != nil {
		release, connErr := h.conns.acquire(claims.UserID, ClientIP(r, h.trustProxy))
		if connErr != nil {
			closeWithCode(conn, websocket.CloseTryAgainLater, connErr.Error(), &wsWriteMu)
			return
		}
		defer release()
	}

	_ = conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	})

	pubsub := h.svc.Subscribe(r.Context(), "lobby_events")
	if pubsub == nil {
		sendError("websocket unavailable")
		return
	}
	defer func() { _ = pubsub.Close() }()

	ch := pubsub.Channel()
	done := make(chan struct{})

	// Write loop
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				wsWriteMu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				wsWriteMu.Unlock()
				if err != nil {
					return
				}
			case msg, ok := <-ch:
				if !ok {
					return
				}
				wsWriteMu.Lock()
				err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
				wsWriteMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	// Read loop (just keeps connection alive and reads pongs/close)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	}

	close(done)
}
```

- [ ] **Step 2: Register the route**

In `cmd/server/main.go`, inside the router section (around line 170), add the new websocket route:

```go
	mux.HandleFunc("GET /games/{id}", handler.GetGameHandler)
	mux.HandleFunc("GET /games/{id}/ws", handler.WSHandler) // Existing Game WebSocket
	mux.HandleFunc("GET /lobby/ws", handler.LobbyWSHandler) // New Lobby WebSocket
	mux.HandleFunc("GET /healthz", api.HealthzHandler)
```

- [ ] **Step 3: Run Go tests**

Run: `go test ./internal/api/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
gofmt -l . && go vet ./internal/api/
git add internal/api/lobby_ws.go cmd/server/main.go
git commit -m "feat(api): add lobby websocket endpoint"
```

---

### Task 3: Frontend - WebSocket Management Hook

**Files:**
- Create: `../mighty-frontend/src/hooks/useLobbyWebSocket.ts`
- Modify: `../mighty-frontend/src/core/types.ts`

**Interfaces:**
- Produces: `useLobbyWebSocket` React hook.

- [ ] **Step 1: Update Server Types**

In `../mighty-frontend/src/core/types.ts`, add the Lobby Event types:

```typescript
export type LobbyEvent = 
  | { type: 'game_created'; game: Game }
  | { type: 'game_joined'; game_id: string; players_seated: number; max_players: number }
```

- [ ] **Step 2: Write the Hook**

Create `../mighty-frontend/src/hooks/useLobbyWebSocket.ts`:

```typescript
import { useEffect, useRef } from 'react'
import { fetchAuthSession } from 'aws-amplify/auth'
import type { LobbyEvent } from '../core/types'

export function useLobbyWebSocket(onEvent: (event: LobbyEvent) => void) {
  const wsRef = useRef<WebSocket | null>(null)
  const isComponentMounted = useRef(true)

  useEffect(() => {
    isComponentMounted.current = true
    let retries = 0

    const connect = async () => {
      if (!isComponentMounted.current) return

      let token = ''
      try {
        const session = await fetchAuthSession()
        token = session.tokens?.accessToken?.toString() ?? ''
      } catch (e) {
        console.warn('Failed to fetch auth session', e)
      }

      const apiUrl = import.meta.env.VITE_API_URL as string | undefined
      let urlStr = ''
      if (apiUrl) {
        const url = new URL(apiUrl)
        const proto = url.protocol === 'https:' ? 'wss:' : 'ws:'
        urlStr = `${proto}//${url.host}/lobby/ws`
      } else {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
        urlStr = `${proto}//${location.host}/lobby/ws`
      }

      const ws = new WebSocket(urlStr)
      wsRef.current = ws

      ws.onopen = () => {
        ws.send(JSON.stringify({ type: 'AUTH', token }))
        retries = 0
      }

      ws.onmessage = (ev) => {
        try {
          const msg = JSON.parse(ev.data) as Record<string, unknown>
          if (msg.type === 'game_created' || msg.type === 'game_joined') {
            onEvent(msg as unknown as LobbyEvent)
          }
        } catch (e) {
          console.error('Failed to parse lobby message', e)
        }
      }

      ws.onclose = () => {
        wsRef.current = null
        if (isComponentMounted.current) {
          const delay = Math.min(1000 * 2 ** retries, 10000)
          retries += 1
          setTimeout(connect, delay)
        }
      }
    }

    void connect()

    return () => {
      isComponentMounted.current = false
      if (wsRef.current) {
        wsRef.current.close()
      }
    }
  }, [onEvent])
}
```

- [ ] **Step 3: Commit**

```bash
cd ../mighty-frontend
npm run lint || true
git add src/hooks/useLobbyWebSocket.ts src/core/types.ts
git commit -m "feat(frontend): add useLobbyWebSocket hook for real-time lobby updates"
cd ../go-mighty
```

---

### Task 4: Frontend - Lobby UI Integration

**Files:**
- Modify: `../mighty-frontend/src/components/LobbyScreen.tsx:21-30`

**Interfaces:**
- Consumes: `useLobbyWebSocket`

- [ ] **Step 1: Replace Polling with WebSocket Hook**

In `../mighty-frontend/src/components/LobbyScreen.tsx`, update the imports at the top:
```typescript
import { useEffect, useState, useCallback } from 'react'
import type { Game, GameConfig, LobbyEvent } from '../core/types'
import { getTableName } from '../core/names'
import { associateWebAuthnCredential, listWebAuthnCredentials } from 'aws-amplify/auth'
import { useLobbyWebSocket } from '../hooks/useLobbyWebSocket'
```

Next, modify `LobbyScreenProps` to allow `games` to be managed internally or update the parent state. Since `games` is currently passed as a prop, the parent component is managing the state and doing the REST fetch. It's safer to let the parent handle the WS updates, or we can manage the array inside `LobbyScreen`.

Wait, we need to inspect the parent component. Assuming the parent manages `games`, we can just call `onRefresh()` when a WebSocket event occurs, OR we can let `LobbyScreen` manage its own `games` list.
Let's modify `LobbyScreenProps` to add a new optional callback `onLobbyEvent?: (e: LobbyEvent) => void`.

Wait, we can just fetch `games` once in the parent, and then the parent merges it. Since we don't know the exact parent component, let's keep it simple: we will just dispatch the WS event to a local state. Actually, if `games` is a prop, we should update the prop. Let's make `onRefresh` just fetch once, and update the parent's state.
*To strictly follow YAGNI and ensure we don't break the parent component, we'll keep the `games` array in the parent but pass `onRefresh` for initial load, and we'll just trigger `onRefresh` immediately for any WS event for now, OR we can introduce local state overrides. Let's implement local state for snappiness:*

In `../mighty-frontend/src/components/LobbyScreen.tsx`, add a local `gamesList` state derived from props:

```typescript
export function LobbyScreen({ games: initialGames, username, onCreate, onJoin, onRefresh, onLogout }: LobbyScreenProps) {
  const [numPlayers, setNumPlayers] = useState(5)
  const [failDist, setFailDist] = useState<GameConfig['fail_dist']>('equal_split')
  const [allowJoker, setAllowJoker] = useState(true)
  const [passkeyStatus, setPasskeyStatus] = useState<string | null>(null)
  const [hasPasskey, setHasPasskey] = useState(false)
  
  const [liveGames, setLiveGames] = useState<Game[]>(initialGames)

  // Sync prop changes (e.g. from initial onRefresh)
  useEffect(() => {
    setLiveGames(initialGames)
  }, [initialGames])

  const handleLobbyEvent = useCallback((event: LobbyEvent) => {
    setLiveGames((prev) => {
      if (event.type === 'game_created') {
        // Prepend new game
        if (prev.some(g => g.id === event.game.id)) return prev
        return [event.game, ...prev]
      } else if (event.type === 'game_joined') {
        // Update seated count or remove if full
        if (event.players_seated >= event.max_players) {
           return prev.filter(g => g.id !== event.game_id)
        }
        return prev.map(g => {
          if (g.id !== event.game_id) return g
          // Create dummy players array to represent seated count
          const updatedPlayers = new Array(event.max_players).fill(null)
          for(let i=0; i<event.players_seated; i++) {
             updatedPlayers[i] = { id: `dummy-${i}`, name: 'Joined', seat: i, is_connected: true }
          }
          return { ...g, players: updatedPlayers }
        })
      }
      return prev
    })
  }, [])

  useLobbyWebSocket(handleLobbyEvent)

  useEffect(() => {
    onRefresh() // Only fetch once on mount
    listWebAuthnCredentials().then(res => {
      if (res.credentials?.length > 0) setHasPasskey(true)
    }).catch(() => {})
  }, [onRefresh])
```

Change the map function inside `LobbyScreen.tsx` to use `liveGames` instead of `games`:
(around line 100)
```typescript
        {liveGames.length === 0 ? (
          <div className="panel" style={{ textAlign: 'center', color: 'var(--color-text-secondary)', padding: '3rem 1rem' }}>
            No tables waiting — create one to start playing.
          </div>
        ) : (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'flex', flexDirection: 'column', gap: '1rem' }}>
            {liveGames.map(g => (
```

- [ ] **Step 2: Check Typescript**

Run: `cd ../mighty-frontend && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
cd ../mighty-frontend
git add src/components/LobbyScreen.tsx
git commit -m "feat(frontend): transition lobby to use incremental websocket updates"
```
