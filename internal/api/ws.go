package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/obs"
	"github.com/joekhosbayar/go-mighty/internal/ratelimit"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/codes"
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

// wsMessageTypeLabel collapses a client-supplied message type to a bounded
// label set. inMsg.Type is arbitrary attacker-controlled JSON; using it
// directly as a metric attribute would let one client mint unlimited series
// and exhaust the free-tier budget, after which data is dropped silently.
func wsMessageTypeLabel(t string) string {
	switch t {
	case WSMessageTypeMove, WSMessageTypeError:
		return t
	default:
		return obs.MsgTypeUnknown
	}
}

// upgraderFor reports origin rejection through originRejected. Upgrade's
// error value cannot distinguish the cause: gorilla's ErrBadHandshake is a
// client-side sentinel that server-side Upgrade never returns, so matching
// on it silently never fires. CheckOrigin runs synchronously on the same
// goroutine as Upgrade, so a plain bool needs no locking.
func (h *Handler) upgraderFor(originRejected *bool) websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			allowed := h.checkOrigin(r)
			if !allowed {
				*originRejected = true
			}

			return allowed
		},
	}
}

// closeWithCode tells the client exactly why the socket is going away before
// hanging up, so a buggy client can back off rather than reconnect-loop into
// the same wall. The write mutex is required because the write loop may be
// mid-frame on the same connection.
func closeWithCode(conn *websocket.Conn, code int, reason string, writeMu *sync.Mutex) {
	writeMu.Lock()
	defer writeMu.Unlock()

	msg := websocket.FormatCloseMessage(code, reason)
	_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
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

	hsCtx, hsSpan := obs.StartWSHandshakeSpan(r.Context(), obs.KindGame)

	var originRejected bool

	up := h.upgraderFor(&originRejected)

	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		obs.Log(hsCtx).Error().Str("game_id", gameID).Err(err).Msg("Failed to upgrade websocket")

		outcome := obs.OutcomeUpgradeFailed
		if originRejected {
			outcome = obs.OutcomeOriginRejected
		}

		h.metrics.RecordHandshake(hsCtx, obs.KindGame, outcome)
		obs.EndWSHandshakeSpan(hsSpan, outcome)

		return
	}
	defer func() { _ = conn.Close() }()

	// Generated once per socket so log lines and message spans for this
	// connection can be correlated, without the connection itself ever
	// becoming a span (see StartWSMessageSpan).
	connID := uuid.NewString()

	conn.SetReadLimit(maxWSMessageBytes)

	var wsWriteMu sync.Mutex

	// sendError takes the context of whatever span is active when the error
	// occurs (hsCtx during the handshake, msgCtx while handling a frame) so
	// the resulting log line carries that span's trace_id.
	sendError := func(ctx context.Context, errMsg string) {
		obs.Log(ctx).Warn().Str("error", errMsg).Msg("Game websocket error")
		if wsErr := h.sendWSError(conn, errMsg, &wsWriteMu); wsErr != nil {
			obs.Log(ctx).Warn().Str("game_id", gameID).Err(wsErr).Msg("Failed to send websocket error")
		}
		closeWithCode(conn, websocket.ClosePolicyViolation, errMsg, &wsWriteMu)
	}

	// 1. Wait for First Message Auth with 5s timeout
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	_, authMessage, err := conn.ReadMessage()
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			// Record the outcome before telling the client: sendError puts
			// the close frame on the wire, and a client that observes that
			// frame is not proof the server is done - see drainToCloseError
			// in ws_hardening_test.go. Recording first is also simply the
			// correct production order: the metric should reflect what the
			// server decided, not what the client happened to observe.
			h.metrics.RecordHandshake(hsCtx, obs.KindGame, obs.OutcomeAuthTimeout)
			obs.EndWSHandshakeSpan(hsSpan, obs.OutcomeAuthTimeout)
			sendError(hsCtx, "auth timed out")
		} else {
			h.metrics.RecordHandshake(hsCtx, obs.KindGame, obs.OutcomeAuthFailed)
			obs.EndWSHandshakeSpan(hsSpan, obs.OutcomeAuthFailed)
			sendError(hsCtx, "failed to read auth message")
		}

		obs.Log(hsCtx).Error().Str("game_id", gameID).Err(err).Msg("Failed to read auth message or timed out")

		return
	}

	var authReq struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(authMessage, &authReq); err != nil || authReq.Type != "AUTH" {
		h.metrics.RecordHandshake(hsCtx, obs.KindGame, obs.OutcomeAuthFailed)
		obs.EndWSHandshakeSpan(hsSpan, obs.OutcomeAuthFailed)
		sendError(hsCtx, "expected AUTH message")

		return
	}

	claims, err := h.authSvc.ValidateToken(r.Context(), authReq.Token)
	if err != nil {
		if errors.Is(err, service.ErrInvalidToken) {
			h.metrics.RecordHandshake(hsCtx, obs.KindGame, obs.OutcomeAuthFailed)
			obs.EndWSHandshakeSpan(hsSpan, obs.OutcomeAuthFailed)
			sendError(hsCtx, "unauthorized")
		} else {
			h.metrics.RecordHandshake(hsCtx, obs.KindGame, obs.OutcomeAuthUnavailable)
			obs.EndWSHandshakeSpan(hsSpan, obs.OutcomeAuthUnavailable)
			sendError(hsCtx, "auth unavailable")
		}

		return
	}

	if h.conns != nil {
		release, connErr := h.conns.acquire(claims.UserID, ClientIP(r, h.trustProxy))
		if connErr != nil {
			obs.Log(hsCtx).Warn().
				Str("game_id", gameID).
				Str("user_id", claims.UserID).
				Err(connErr).
				Msg("Rejected websocket: connection cap reached")

			outcome := obs.OutcomeConnLimitIP
			if errors.Is(connErr, errTooManyUserConns) {
				outcome = obs.OutcomeConnLimitUser
			}

			h.metrics.RecordHandshake(hsCtx, obs.KindGame, outcome)
			obs.EndWSHandshakeSpan(hsSpan, outcome)
			closeWithCode(conn, websocket.CloseTryAgainLater, connErr.Error(), &wsWriteMu)

			return
		}

		defer release()
	}

	h.metrics.RecordHandshake(hsCtx, obs.KindGame, obs.OutcomeOK)
	obs.EndWSHandshakeSpan(hsSpan, obs.OutcomeOK)
	h.metrics.AddConnection(r.Context(), obs.KindGame, 1)

	defer h.metrics.AddConnection(r.Context(), obs.KindGame, -1)

	// 2. Swap the auth deadline for a rolling idle deadline. A pong or any
	// inbound message refreshes it; a silent socket is reaped after
	// wsIdleTimeout instead of pinning a goroutine forever.
	_ = conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	})

	pubsub := h.svc.Subscribe(r.Context(), gameID)
	if pubsub == nil {
		sendError(hsCtx, "websocket unavailable")
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

				// The relay is deliberately not a passthrough. Decoding into
				// the envelope type and re-encoding is what makes redaction
				// unavoidable: a field a future publisher adds but does not
				// declare on GameEvent is dropped here rather than leaked.
				var ev service.GameEvent
				if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
					obs.Log(r.Context()).Error().
						Str("game_id", gameID).
						Err(err).
						Msg("Dropping unparseable game event")

					continue
				}

				out, err := json.Marshal(ev.RedactFor(claims.UserID))
				if err != nil {
					obs.Log(r.Context()).Error().
						Str("game_id", gameID).
						Err(err).
						Msg("Dropping unencodable game event")

					continue
				}

				wsWriteMu.Lock()
				err = conn.WriteMessage(websocket.TextMessage, out)
				wsWriteMu.Unlock()

				if err != nil {
					return
				}
			}
		}
	}()

	var msgBucket *ratelimit.Bucket
	if h.wsMessagesPerSec > 0 && h.wsMessageBurst > 0 {
		msgBucket = ratelimit.NewBucket(h.wsMessageBurst, h.wsMessagesPerSec, time.Now())
	}

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

		if msgBucket != nil && !msgBucket.Allow(time.Now()) {
			log.Warn().
				Str("game_id", gameID).
				Str("user_id", claims.UserID).
				Msg("WebSocket message rate exceeded, closing socket")
			closeWithCode(conn, websocket.ClosePolicyViolation, "rate limit exceeded", &wsWriteMu)
			h.metrics.RecordWSMessage(r.Context(), obs.MsgTypeUnknown, obs.MsgRateLimited)

			break
		}

		var inMsg IncomingWSMessage
		if err := json.Unmarshal(message, &inMsg); err != nil {
			// No message span here: a frame that cannot even be typed gets
			// no per-message span, only a connection-scoped log line.
			sendError(r.Context(), "invalid message format")
			h.metrics.RecordWSMessage(r.Context(), obs.MsgTypeUnknown, obs.MsgInvalid)

			continue
		}

		// A new ROOT span per inbound frame, not a child of any
		// connection-scoped span: the connection lives for a whole game, and
		// a span that long is unusable in Tempo and pins SDK memory for
		// hours. Ended explicitly on every exit path below - never deferred,
		// which inside this loop would hold every message's span open until
		// the socket closes.
		//
		// The span NAME is built from the sanitized label, not inMsg.Type
		// directly: inMsg.Type is unbounded attacker-controlled JSON (up to
		// maxWSMessageBytes), and Grafana Cloud's span-metrics generator
		// turns span_name into a metric series by default, so an unbounded
		// name would reopen the same cardinality hole wsMessageTypeLabel was
		// written to close for the mighty.ws.messages metric below.
		msgTypeLabel := wsMessageTypeLabel(inMsg.Type)
		msgCtx, msgSpan := obs.StartWSMessageSpan(r.Context(), msgTypeLabel, gameID, claims.UserID, connID)

		if inMsg.Type == WSMessageTypeMove {
			convertedPayload, err := ConvertPayload(inMsg.MoveType, inMsg.Payload)
			if err != nil {
				sendError(msgCtx, "invalid payload structure: "+err.Error())
				h.metrics.RecordWSMessage(r.Context(), msgTypeLabel, obs.MsgInvalid)
				msgSpan.RecordError(err)
				msgSpan.SetStatus(codes.Error, err.Error())
				msgSpan.End()

				continue
			}

			h.metrics.RecordWSMessage(r.Context(), msgTypeLabel, obs.MsgAccepted)

			_, err = h.svc.ProcessMove(msgCtx, gameID, claims.UserID, inMsg.MoveType, convertedPayload, inMsg.ClientVersion)
			if err != nil {
				sendError(msgCtx, err.Error())
				msgSpan.RecordError(err)
				msgSpan.SetStatus(codes.Error, err.Error())
				msgSpan.End()

				continue
			}
			// On success, the GameService publishes an event via Redis,
			// which the write loop will pick up and send to all connected clients.
		} else {
			// Non-MOVE frames (e.g. a client-sent "ERROR" echo or anything
			// else that parses as JSON) still reach here and are still
			// accepted traffic - record them so mighty.ws.messages counts
			// what its name says instead of undercounting to "accepted
			// moves only".
			h.metrics.RecordWSMessage(r.Context(), msgTypeLabel, obs.MsgAccepted)
		}

		msgSpan.End()
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
