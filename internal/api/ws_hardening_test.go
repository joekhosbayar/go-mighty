package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/obs"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// wsTestOrigin and wsTestMovePass are named so the metric tests below reusing
// these literals don't push either string over goconst's duplicate threshold
// (the pre-existing literal tests in this file already sit right at it).
const (
	wsTestOrigin   = "https://themighty.gg"
	wsTestMovePass = "pass"
)

// serveWS mounts handler on a test server at the websocket route.
func serveWS(t *testing.T, handler *Handler) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/games/{id}/ws", handler.WSHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

// dialWSWithOrigin dials without the auth step, returning the handshake error
// and response so origin rejection can be asserted.
func dialWSWithOrigin(t *testing.T, server *httptest.Server, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/games/game-1/ws"

	header := http.Header{}
	header.Set("Origin", origin)

	conn, resp, err := websocket.DefaultDialer.DialContext(t.Context(), wsURL, header)
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}

	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}

	return conn, resp, err
}

func TestWSHandlerRejectsDisallowedOrigin(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithAllowedOrigins([]string{"https://themighty.gg"})(handler)

	server := serveWS(t, handler)

	_, resp, err := dialWSWithOrigin(t, server, "https://evil.example")
	if err == nil {
		t.Fatal("expected the handshake to be rejected")
	}

	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %+v", resp)
	}
}

func TestWSHandlerAcceptsAllowedOrigin(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithAllowedOrigins([]string{"https://themighty.gg", "https://www.themighty.gg"})(handler)

	server := serveWS(t, handler)

	conn, _, err := dialWSWithOrigin(t, server, "https://www.themighty.gg")
	if err != nil {
		t.Fatalf("expected the handshake to succeed, got %v", err)
	}

	if conn == nil {
		t.Fatal("expected a connection")
	}
}

func TestWSHandlerAllowsCrossOriginlessClients(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithAllowedOrigins([]string{"https://themighty.gg"})(handler)

	server := serveWS(t, handler)

	// Native clients (the Swift app) send no Origin at all; they authenticate
	// with a token instead, so the origin check must not block them.
	_, _, err := dialWSWithOrigin(t, server, "")
	if err != nil {
		t.Fatalf("expected an Origin-less handshake to succeed, got %v", err)
	}
}

func TestWSHandlerClosesConnectionOnOversizedFrame(t *testing.T) {
	t.Parallel()

	server, _ := setupWSTestServer(t)
	conn := dialWS(t, server, "/games/game-1/ws", generateValidToken("user-1", "alice"))

	oversized := strings.Repeat("a", int(maxWSMessageBytes)+1024)
	if err := conn.WriteText(oversized); err != nil {
		t.Fatalf("failed to write oversized frame: %v", err)
	}

	if _, _, err := conn.Conn.ReadMessage(); err == nil {
		t.Fatal("expected the connection to be closed after an oversized frame")
	}
}

func TestWSHandlerClosesConnectionOnMessageFlood(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithWSMessageRate(2, 2)(handler)

	server := serveWS(t, handler)
	conn := dialWS(t, server, "/games/game-1/ws", generateValidToken("user-1", "alice"))

	// Burst 2 means the third immediate move is over budget.
	for range 5 {
		_ = conn.WriteJSON(map[string]any{
			keyType:          WSMessageTypeMove,
			keyMoveType:      "pass",
			"payload":        nil,
			"client_version": 1,
		})
	}

	_ = conn.Conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var closeErr *websocket.CloseError

	for {
		_, _, err := conn.Conn.ReadMessage()
		if err != nil {
			if !errors.As(err, &closeErr) {
				t.Fatalf("expected a websocket close error, got %v", err)
			}

			break
		}
	}

	if closeErr.Code != websocket.ClosePolicyViolation {
		t.Fatalf("expected close code %d, got %d", websocket.ClosePolicyViolation, closeErr.Code)
	}
}

func TestWSHandlerRejectsExcessConnectionsForOneUser(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithConnLimits(1, 100)(handler)

	server := serveWS(t, handler)
	token := generateValidToken("user-1", "alice")

	first := dialWS(t, server, "/games/game-1/ws", token)

	// Keep the first socket alive; the second must be refused.
	second := dialWS(t, server, "/games/game-1/ws", token)

	_ = second.Conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var closeErr *websocket.CloseError

	for {
		_, _, err := second.Conn.ReadMessage()
		if err != nil {
			if !errors.As(err, &closeErr) {
				t.Fatalf("expected a websocket close error, got %v", err)
			}

			break
		}
	}

	if closeErr.Code != websocket.CloseTryAgainLater {
		t.Fatalf("expected close code %d, got %d", websocket.CloseTryAgainLater, closeErr.Code)
	}

	_ = first.Conn.Close()
}

// wsMetricsHandler returns a handler wired to a manual metric reader, plus a
// counts function that reads one instrument's totals keyed by an attribute.
//
// setupWSTestHandler builds the handler with no options, and the existing
// tests in this file apply options post-construction (see
// TestWSHandlerRejectsDisallowedOrigin), so WithMetrics is applied the same way.
func wsMetricsHandler(t *testing.T) (*Handler, func(instrument, attrKey string) map[string]int64) {
	t.Helper()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	m, err := obs.NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	WithMetrics(m)(handler)

	counts := func(instrument, attrKey string) map[string]int64 {
		t.Helper()

		var rm metricdata.ResourceMetrics
		if collectErr := reader.Collect(context.Background(), &rm); collectErr != nil {
			t.Fatalf("collect: %v", collectErr)
		}

		out := map[string]int64{}

		for _, sm := range rm.ScopeMetrics {
			for _, mm := range sm.Metrics {
				if mm.Name != instrument {
					continue
				}

				sum, ok := mm.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("%s is not an int64 sum", instrument)
				}

				for _, p := range sum.DataPoints {
					if v, found := p.Attributes.Value(attribute.Key(attrKey)); found {
						out[v.AsString()] += p.Value
					}
				}
			}
		}

		return out
	}

	return handler, counts
}

// drainToCloseError reads from conn until it errors, returning the close
// error. It blocks until the server has actually processed the scenario under
// test and closed the socket, which is the synchronisation signal called for
// here instead of a fixed sleep: handshake metrics are recorded on the server
// goroutine, so collecting them immediately after a client-side dial can
// race.
func drainToCloseError(t *testing.T, conn *websocket.Conn, timeout time.Duration) *websocket.CloseError {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(timeout))

	var closeErr *websocket.CloseError

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if !errors.As(err, &closeErr) {
				t.Fatalf("expected a websocket close error, got %v", err)
			}

			break
		}
	}

	return closeErr
}

func TestHandshakeMetricAuthTimeout(t *testing.T) {
	t.Parallel()

	handler, counts := wsMetricsHandler(t)
	server := serveWS(t, handler)

	conn, _, err := dialWSWithOrigin(t, server, "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Send nothing; wait for the server's 5s auth deadline to fire and close
	// the socket rather than sleeping a fixed duration ourselves. The margin
	// above 5s is generous headroom for a loaded CI box, not a race.
	_ = drainToCloseError(t, conn, 20*time.Second)

	got := counts("mighty.ws.handshake", "outcome")
	if got[obs.OutcomeAuthTimeout] != 1 {
		t.Fatalf("expected 1 auth_timeout, got %v", got)
	}
}

func TestHandshakeMetricAuthFailed(t *testing.T) {
	t.Parallel()

	handler, counts := wsMetricsHandler(t)
	// A nil-claims fakeValidator fails every token, including a non-empty
	// one, with service.ErrInvalidToken.
	handler.authSvc = &fakeValidator{}

	server := serveWS(t, handler)

	conn, _, err := dialWSWithOrigin(t, server, "")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"AUTH","token":"bad"}`)); err != nil {
		t.Fatalf("failed to write auth message: %v", err)
	}

	_ = drainToCloseError(t, conn, 2*time.Second)

	got := counts("mighty.ws.handshake", "outcome")
	if got[obs.OutcomeAuthFailed] != 1 {
		t.Fatalf("expected 1 auth_failed, got %v", got)
	}
}

func TestHandshakeMetricOriginRejected(t *testing.T) {
	t.Parallel()

	handler, counts := wsMetricsHandler(t)
	WithAllowedOrigins([]string{wsTestOrigin})(handler)

	server := serveWS(t, handler)

	_, resp, err := dialWSWithOrigin(t, server, "https://evil.example")
	if err == nil {
		t.Fatal("expected the handshake to be rejected")
	}

	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %+v", resp)
	}

	got := counts("mighty.ws.handshake", "outcome")
	if got[obs.OutcomeOriginRejected] != 1 {
		t.Fatalf("expected 1 origin_rejected, got %v", got)
	}

	// The regression this guards against: Upgrade's error value cannot
	// distinguish an origin rejection from any other handshake failure, so a
	// naive implementation double-counts this as upgrade_failed too.
	if got[obs.OutcomeUpgradeFailed] != 0 {
		t.Fatalf("expected 0 upgrade_failed, got %v", got)
	}
}

// TestHandshakeMetricUpgradeFailedNotOrigin drives a handshake failure that
// has nothing to do with CheckOrigin (no Sec-WebSocket-* headers at all, so
// gorilla's Upgrade rejects it before CheckOrigin is ever called) and asserts
// it is counted as upgrade_failed, not origin_rejected. Paired with
// TestHandshakeMetricOriginRejected's new assertion above, this is what
// proves the two outcomes are mutually exclusive rather than one leaking
// into the other.
func TestHandshakeMetricUpgradeFailedNotOrigin(t *testing.T) {
	t.Parallel()

	handler, counts := wsMetricsHandler(t)
	server := serveWS(t, handler)

	// A plain GET carries none of the Connection/Upgrade/Sec-WebSocket-*
	// headers gorilla requires, so Upgrade rejects it on the very first
	// check, before CheckOrigin runs at all.
	resp, err := http.Get(server.URL + "/games/game-1/ws") //nolint:noctx // test-only, mirrors http.Get usage already in this package
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusSwitchingProtocols {
		t.Fatalf("expected the handshake to be rejected, got %d", resp.StatusCode)
	}

	got := counts("mighty.ws.handshake", "outcome")
	if got[obs.OutcomeUpgradeFailed] != 1 {
		t.Fatalf("expected 1 upgrade_failed, got %v", got)
	}

	if got[obs.OutcomeOriginRejected] != 0 {
		t.Fatalf("expected 0 origin_rejected, got %v", got)
	}
}

func TestHandshakeMetricConnLimitUser(t *testing.T) {
	t.Parallel()

	handler, counts := wsMetricsHandler(t)

	svc, ok := handler.svc.(*fakeWSGameService)
	if !ok {
		t.Fatal("expected fake websocket game service")
	}

	WithConnLimits(1, 0)(handler)

	server := serveWS(t, handler)
	token := generateValidToken("user-1", "alice")

	first := dialWS(t, server, "/games/game-1/ws", token)

	// A completed move round trip proves the first connection passed the
	// OutcomeOK point (which precedes the read loop) before metrics are
	// collected below.
	if err := first.WriteJSON(map[string]any{
		keyType:          WSMessageTypeMove,
		keyMoveType:      wsTestMovePass,
		"payload":        nil,
		"client_version": 1,
	}); err != nil {
		t.Fatalf("failed to write move on first connection: %v", err)
	}

	if !svc.WaitForProcessMove(2 * time.Second) {
		t.Fatal("expected ProcessMove to be called on the first connection")
	}

	// Keep the first socket alive; the second must be refused.
	second := dialWS(t, server, "/games/game-1/ws", token)

	closeErr := drainToCloseError(t, second.Conn, 2*time.Second)
	if closeErr.Code != websocket.CloseTryAgainLater {
		t.Fatalf("expected close code %d, got %d", websocket.CloseTryAgainLater, closeErr.Code)
	}

	got := counts("mighty.ws.handshake", "outcome")
	if got[obs.OutcomeConnLimitUser] != 1 {
		t.Fatalf("expected 1 conn_limit_user, got %v", got)
	}

	if got[obs.OutcomeOK] != 1 {
		t.Fatalf("expected 1 ok, got %v", got)
	}

	_ = first.Conn.Close()
}

func TestHandshakeMetricConnLimitIP(t *testing.T) {
	t.Parallel()

	handler, counts := wsMetricsHandler(t)
	WithConnLimits(0, 1)(handler)

	server := serveWS(t, handler)
	token := generateValidToken("user-1", "alice")

	first := dialWS(t, server, "/games/game-1/ws", token)
	second := dialWS(t, server, "/games/game-1/ws", token)

	closeErr := drainToCloseError(t, second.Conn, 2*time.Second)
	if closeErr.Code != websocket.CloseTryAgainLater {
		t.Fatalf("expected close code %d, got %d", websocket.CloseTryAgainLater, closeErr.Code)
	}

	got := counts("mighty.ws.handshake", "outcome")
	if got[obs.OutcomeConnLimitIP] != 1 {
		t.Fatalf("expected 1 conn_limit_ip, got %v", got)
	}

	_ = first.Conn.Close()
}

func TestHandshakeMetricOK(t *testing.T) {
	t.Parallel()

	handler, counts := wsMetricsHandler(t)

	svc, ok := handler.svc.(*fakeWSGameService)
	if !ok {
		t.Fatal("expected fake websocket game service")
	}

	server := serveWS(t, handler)
	token := generateValidToken("user-1", "alice")

	conn := dialWS(t, server, "/games/game-1/ws", token)

	// A completed move round trip is the "successful read/write" signal that
	// the connection passed the OutcomeOK point before metrics are collected.
	if err := conn.WriteJSON(map[string]any{
		keyType:          WSMessageTypeMove,
		keyMoveType:      wsTestMovePass,
		"payload":        nil,
		"client_version": 1,
	}); err != nil {
		t.Fatalf("failed to write move: %v", err)
	}

	if !svc.WaitForProcessMove(2 * time.Second) {
		t.Fatal("expected ProcessMove to be called")
	}

	got := counts("mighty.ws.handshake", "outcome")
	if got[obs.OutcomeOK] != 1 {
		t.Fatalf("expected 1 ok, got %v", got)
	}
}

func TestWSMessageMetricInvalidFrame(t *testing.T) {
	t.Parallel()

	handler, counts := wsMetricsHandler(t)
	server := serveWS(t, handler)
	token := generateValidToken("user-1", "alice")

	conn := dialWS(t, server, "/games/game-1/ws", token)

	if err := conn.WriteText("not json"); err != nil {
		t.Fatalf("failed to write invalid frame: %v", err)
	}

	// The server's error response confirms the invalid-frame branch (and the
	// metric recording that precedes it) has already run.
	msg := conn.ReadText(t)
	if msg.Type != WSMessageTypeError || !strings.Contains(msg.Error, "invalid message format") {
		t.Fatalf("unexpected ws error response: %+v", msg)
	}

	got := counts("mighty.ws.messages", "outcome")
	if got[obs.MsgInvalid] != 1 {
		t.Fatalf("expected 1 invalid message, got %v", got)
	}
}
