package api

import (
	"encoding/json"
	"net/http"
	"net/url"
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
	claims, err := h.authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	gameID := r.PathValue("id")

	pubsub := h.svc.Subscribe(r.Context(), gameID)
	if pubsub == nil {
		http.Error(w, "websocket unavailable", http.StatusServiceUnavailable)
		return
	}
	defer pubsub.Close()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Str("game_id", gameID).Str("user_id", claims.UserID).Err(err).Msg("Failed to upgrade websocket")
		return
	}
	defer conn.Close()

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
				err := conn.WriteMessage(websocket.PingMessage, nil)
				if err != nil {
					return
				}
			case msg, ok := <-ch:
				if !ok {
					return // pubsub closed
				}
				// msg.Payload is the JSON string from Redis
				err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
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
			h.sendWSError(conn, "invalid message format")
			continue
		}

		if inMsg.Type == "MOVE" {
			convertedPayload, err := ConvertPayload(inMsg.MoveType, inMsg.Payload)
			if err != nil {
				h.sendWSError(conn, "invalid payload structure: "+err.Error())
				continue
			}

			// Process the move
			// We use context.Background() here because the request context might be cancelled
			// if the HTTP handler returns, but we want the move to complete. 
			// Wait, WSHandler doesn't return until the connection is closed. So r.Context() is fine.
			_, err = h.svc.ProcessMove(r.Context(), gameID, claims.UserID, inMsg.MoveType, convertedPayload, inMsg.ClientVersion)
			if err != nil {
				h.sendWSError(conn, err.Error())
				continue
			}
			// On success, the GameService publishes an event via Redis, 
			// which the write loop will pick up and send to all connected clients.
		}
	}
	
	close(done)
}

func (h *Handler) sendWSError(conn *websocket.Conn, errMsg string) {
	errPayload := OutgoingWSError{
		Type:  "ERROR",
		Error: errMsg,
	}
	data, _ := json.Marshal(errPayload)
	conn.WriteMessage(websocket.TextMessage, data)
}
