package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		// Allow non-browser clients that may not set Origin.
		if origin == "" {
			return true
		}

		u, err := url.Parse(origin)
		if err != nil {
			return false
		}

		// Only allow requests from the same host.
		// Note: This doesn't validate scheme (http vs https) or port to allow
		// flexible development environments. For production, consider using
		// environment-specific allowlists of trusted origins.
		return u.Host == r.Host
	},
}

// IncomingWSMessage defines the structure of messages sent by the client over WebSocket
type IncomingWSMessage struct {
	Type          string        `json:"type"` // e.g., "MOVE"
	MoveType      game.MoveType `json:"move_type"`
	Payload       interface{}   `json:"payload"`
	ClientVersion int64         `json:"client_version"`
}

// OutgoingWSError defines the structure of error messages sent to the client
type OutgoingWSError struct {
	Type  string `json:"type"` // "ERROR"
	Error string `json:"error"`
}

// WSHandler handles websocket connections
func (h *Handler) WSHandler(w http.ResponseWriter, r *http.Request) {
	gameID := r.PathValue("id")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Str("game_id", gameID).Err(err).Msg("Failed to upgrade websocket")
		return
	}
	defer conn.Close()

	var wsWriteMu sync.Mutex
	sendError := func(errMsg string) {
		if wsErr := h.sendWSError(conn, errMsg, &wsWriteMu); wsErr != nil {
			log.Warn().Str("game_id", gameID).Err(wsErr).Msg("Failed to send websocket error")
		}
	}

	// 1. Wait for First Message Auth with 5s timeout
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, authMessage, err := conn.ReadMessage()
	if err != nil {
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

	claims, err := h.authSvc.ValidateToken(authReq.Token)
	if err != nil {
		sendError("unauthorized")
		return
	}

	// 2. Reset deadline after successful auth
	conn.SetReadDeadline(time.Time{})

	pubsub := h.svc.Subscribe(r.Context(), gameID)
	if pubsub == nil {
		sendError("websocket unavailable")
		return
	}
	defer pubsub.Close()

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

		var inMsg IncomingWSMessage
		if err := json.Unmarshal(message, &inMsg); err != nil {
			sendError("invalid message format")
			continue
		}

		if inMsg.Type == "MOVE" {
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
		Type:  "ERROR",
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
