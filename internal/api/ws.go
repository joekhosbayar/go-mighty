package api

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
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

// WSHandler handles websocket connections
func (h *Handler) WSHandler(w http.ResponseWriter, r *http.Request) {
	gameID := r.PathValue("id")

	pubsub := h.svc.Subscribe(r.Context(), gameID)
	if pubsub == nil {
		http.Error(w, "websocket unavailable", http.StatusServiceUnavailable)
		return
	}
	defer pubsub.Close()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Str("game_id", gameID).Err(err).Msg("Failed to upgrade websocket")
		return
	}
	defer conn.Close()

	ch := pubsub.Channel()

	// Heartbeat
	go func() {
		for {
			time.Sleep(30 * time.Second)
			err := conn.WriteMessage(websocket.PingMessage, nil)
			if err != nil {
				return
			}
		}
	}()

	for msg := range ch {
		// msg.Payload is the JSON string
		err := conn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
		if err != nil {
			break
		}
	}
}
