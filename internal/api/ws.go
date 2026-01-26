package api

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // Allow all origins for now
}

// WSHandler handles websocket connections
func (h *Handler) WSHandler(w http.ResponseWriter, r *http.Request) {
	gameID := r.PathValue("id")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
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
