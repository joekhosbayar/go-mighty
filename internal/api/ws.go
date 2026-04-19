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

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Str("game_id", gameID).Err(err).Msg("Failed to upgrade websocket")
		return
	}
	defer conn.Close()

	// Subscribe to redis
	// We need access to redis store from handler.
	// HACK: We access redis store via service.
	// Ideally Service exposes Subscribe method or we pass Store to Handler.
	// I'll assume I can add `GetRedisStore()` to service.
	// Or I'll just change Handler struct to include redis store if needed?
	// But Service encapsulates it.
	// Actually `redis.Store` has `Subscribe`.
	// I will add `Subscribe(ctx, gameID)` to `GameService`.

	pubsub := h.svc.Subscribe(r.Context(), gameID)
	if pubsub == nil {
		return
	}
	defer pubsub.Close()

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
