package api

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/obs"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/rs/zerolog/log"
)

// LobbyWSHandler handles websocket connections for the global lobby feed.
func (h *Handler) LobbyWSHandler(w http.ResponseWriter, r *http.Request) {
	up := h.upgrader()

	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to upgrade lobby websocket")

		if !errors.Is(err, websocket.ErrBadHandshake) {
			h.metrics.RecordHandshake(r.Context(), obs.KindLobby, obs.OutcomeUpgradeFailed)
		}

		return
	}
	defer func() { _ = conn.Close() }()

	conn.SetReadLimit(maxWSMessageBytes)

	var wsWriteMu sync.Mutex
	sendError := func(errMsg string) {
		log.Warn().Str("error", errMsg).Msg("Lobby websocket auth failed")
		if wsErr := h.sendWSError(conn, errMsg, &wsWriteMu); wsErr != nil {
			log.Warn().Err(wsErr).Msg("Failed to send lobby websocket error")
		}
		closeWithCode(conn, websocket.ClosePolicyViolation, errMsg, &wsWriteMu)
	}

	// 1. Wait for First Message Auth with 5s timeout
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	_, authMessage, err := conn.ReadMessage()
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			sendError("auth timed out")
			h.metrics.RecordHandshake(r.Context(), obs.KindLobby, obs.OutcomeAuthTimeout)
		} else {
			sendError("failed to read auth message")
			h.metrics.RecordHandshake(r.Context(), obs.KindLobby, obs.OutcomeAuthFailed)
		}
		return
	}

	var authReq struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(authMessage, &authReq); err != nil || authReq.Type != "AUTH" {
		sendError("expected AUTH message")
		h.metrics.RecordHandshake(r.Context(), obs.KindLobby, obs.OutcomeAuthFailed)

		return
	}

	claims, err := h.authSvc.ValidateToken(r.Context(), authReq.Token)
	if err != nil {
		if errors.Is(err, service.ErrInvalidToken) {
			sendError("unauthorized")
			h.metrics.RecordHandshake(r.Context(), obs.KindLobby, obs.OutcomeAuthFailed)
		} else {
			sendError("auth unavailable")
			h.metrics.RecordHandshake(r.Context(), obs.KindLobby, obs.OutcomeAuthUnavailable)
		}
		return
	}

	if h.conns != nil {
		release, connErr := h.conns.acquire(claims.UserID, ClientIP(r, h.trustProxy))
		if connErr != nil {
			outcome := obs.OutcomeConnLimitIP
			if errors.Is(connErr, errTooManyUserConns) {
				outcome = obs.OutcomeConnLimitUser
			}

			h.metrics.RecordHandshake(r.Context(), obs.KindLobby, outcome)
			closeWithCode(conn, websocket.CloseTryAgainLater, connErr.Error(), &wsWriteMu)

			return
		}
		defer release()
	}

	h.metrics.RecordHandshake(r.Context(), obs.KindLobby, obs.OutcomeOK)
	h.metrics.AddConnection(r.Context(), obs.KindLobby, 1)

	defer h.metrics.AddConnection(r.Context(), obs.KindLobby, -1)

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
