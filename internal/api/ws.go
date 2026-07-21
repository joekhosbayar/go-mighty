package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/rs/zerolog/log"
)

const (
	WSMessageTypeMove  = "MOVE"
	WSMessageTypeError = "ERROR"
)

const (
	// maxWSMessageBytes caps a single inbound frame. The largest legitimate
	// client message is a move of a few hundred bytes; 32KB is generous
	// headroom that still makes memory exhaustion via one socket impossible.
	maxWSMessageBytes int64 = 32 << 10

	// wsIdleTimeout drops a socket that has sent neither a message nor a pong
	// within this window. The write loop pings every 30s, so a healthy client
	// refreshes the deadline twice per window.
	wsIdleTimeout = 60 * time.Second
)

// checkOrigin enforces the configured origin allowlist for WebSocket
// upgrades. Browsers always send Origin; native clients (Electron/Swift) may
// not, and those are gated by the AUTH token instead.
func (h *Handler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	if len(h.allowedOrigins) == 0 {
		// Dev default: same-host only, matching the pre-allowlist behaviour.
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}

		return u.Host == r.Host
	}

	candidate := strings.ToLower(strings.TrimSuffix(origin, "/"))
	for _, allowed := range h.allowedOrigins {
		if candidate == allowed {
			return true
		}
	}

	log.Warn().Str("origin", origin).Msg("Rejected websocket upgrade from disallowed origin")

	return false
}

func (h *Handler) upgrader() websocket.Upgrader {
	return websocket.Upgrader{CheckOrigin: h.checkOrigin}
}

// IncomingWSMessage defines the structure of messages sent by the client over WebSocket.
type IncomingWSMessage struct {
	Type          string        `json:"type"` // e.g., "MOVE"
	MoveType      game.MoveType `json:"move_type"`
	Payload       any           `json:"payload"`
	ClientVersion int64         `json:"client_version"`
}

// OutgoingWSError defines the structure of error messages sent to the client.
type OutgoingWSError struct {
	Type  string `json:"type"` // "ERROR"
	Error string `json:"error"`
}

// WSHandler handles websocket connections.
func (h *Handler) WSHandler(w http.ResponseWriter, r *http.Request) {
	gameID := r.PathValue("id")

	up := h.upgrader()

	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Str("game_id", gameID).Err(err).Msg("Failed to upgrade websocket")
		return
	}
	defer func() { _ = conn.Close() }()

	conn.SetReadLimit(maxWSMessageBytes)

	var wsWriteMu sync.Mutex

	sendError := func(errMsg string) {
		if wsErr := h.sendWSError(conn, errMsg, &wsWriteMu); wsErr != nil {
			log.Warn().Str("game_id", gameID).Err(wsErr).Msg("Failed to send websocket error")
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

		log.Error().Str("game_id", gameID).Err(err).Msg("Failed to read auth message or timed out")

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

	// 2. Swap the auth deadline for a rolling idle deadline. A pong or any
	// inbound message refreshes it; a silent socket is reaped after
	// wsIdleTimeout instead of pinning a goroutine forever.
	_ = conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	})

	pubsub := h.svc.Subscribe(r.Context(), gameID)
	if pubsub == nil {
		sendError("websocket unavailable")
		return
	}
	defer func() { _ = pubsub.Close() }()

	ch := pubsub.Channel()

	// Create a channel to signal connection closure
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
					return // pubsub closed
				}
				// msg.Payload is the JSON string from Redis
				wsWriteMu.Lock()
				err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
				wsWriteMu.Unlock()

				if err != nil {
					return
				}
			}
		}
	}()

	// Read loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Error().Str("game_id", gameID).Str("user_id", claims.UserID).Err(err).Msg("WebSocket read error")
			}

			break
		}

		_ = conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))

		var inMsg IncomingWSMessage
		if err := json.Unmarshal(message, &inMsg); err != nil {
			sendError("invalid message format")
			continue
		}

		if inMsg.Type == WSMessageTypeMove {
			convertedPayload, err := ConvertPayload(inMsg.MoveType, inMsg.Payload)
			if err != nil {
				sendError("invalid payload structure: " + err.Error())
				continue
			}

			_, err = h.svc.ProcessMove(r.Context(), gameID, claims.UserID, inMsg.MoveType, convertedPayload, inMsg.ClientVersion)
			if err != nil {
				sendError(err.Error())
				continue
			}
			// On success, the GameService publishes an event via Redis,
			// which the write loop will pick up and send to all connected clients.
		}
	}

	close(done)
}

func (h *Handler) sendWSError(conn *websocket.Conn, errMsg string, writeMu *sync.Mutex) error {
	errPayload := OutgoingWSError{
		Type:  WSMessageTypeError,
		Error: errMsg,
	}

	data, err := json.Marshal(errPayload)
	if err != nil {
		return err
	}

	writeMu.Lock()
	defer writeMu.Unlock()

	return conn.WriteMessage(websocket.TextMessage, data)
}
