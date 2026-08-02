package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// otelhttp's ResponseWriter wrapper does not reliably implement
// http.Hijacker, which gorilla/websocket needs to take over the connection.
// TraceFilter must therefore exclude every WS route. If this test fails,
// production WebSockets are broken.
func TestWebSocketUpgradeSurvivesOtelHTTPWrapping(t *testing.T) {
	t.Parallel()

	upgraded := make(chan struct{}, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /games/{id}/ws", func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		upgraded <- struct{}{}
	})

	wrapped := otelhttp.NewHandler(mux, "mighty", otelhttp.WithFilter(TraceFilter))
	srv := httptest.NewServer(wrapped)

	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/games/abc/ws"

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err, "upgrade failed through otelhttp: TraceFilter must exclude WS routes")

	defer func() { _ = conn.Close() }()

	require.Len(t, upgraded, 1)
}

// TestWebSocketUpgradeSurvivesFullMiddlewareChain confirms the WS upgrade
// still works through the genuine production chain — not just a bare
// otelhttp.NewHandler(mux) like the test above, but the exact composition
// cmd/server/main.go builds: otelhttp.NewHandler wrapping
// Handler.LoggingMiddleware wrapping BodyLimitMiddleware wrapping the mux.
// Both LoggingResponseWriter (LoggingMiddleware's wrapper) and
// BodyLimitMiddleware sit *inside* otelhttp's own wrapper for every request,
// so the http.Hijacker requirement has to survive all three layers, not one.
// This is a regression guard: it exists specifically to catch a future
// change to LoggingResponseWriter that stops forwarding Hijack() (verified
// by deliberately breaking that forwarding and confirming this test fails —
// see task-9-report.md).
func TestWebSocketUpgradeSurvivesFullMiddlewareChain(t *testing.T) {
	t.Parallel()

	upgraded := make(chan struct{}, 1)

	wsHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		upgraded <- struct{}{}
	})

	mux := http.NewServeMux()
	mux.Handle("GET /games/{id}/ws", wsHandler)

	h := &Handler{}

	rootHandler := h.LoggingMiddleware(BodyLimitMiddleware(mux))
	rootHandler = otelhttp.NewHandler(rootHandler, "mighty", otelhttp.WithFilter(TraceFilter))

	srv := httptest.NewServer(rootHandler)

	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/games/abc/ws"

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err, "upgrade failed through the full production chain: otelhttp -> LoggingMiddleware -> BodyLimitMiddleware -> mux")

	defer func() { _ = conn.Close() }()

	require.Len(t, upgraded, 1)
}

func TestTraceFilterExcludesHealthzAndWS(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]bool{
		"/healthz":        false,
		"/games/abc/ws":   false,
		"/lobby/ws":       false,
		"/games":          true,
		"/games/abc":      true,
		"/games/abc/join": true,
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		require.Equal(t, want, TraceFilter(req), "path %s", path)
	}
}
