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
// still works when composed exactly as main.go does: routes registered on a
// standard-library http.ServeMux using Go 1.22+ method+pattern syntax, with
// the whole mux wrapped once in otelhttp.NewHandler(..., otelhttp.WithFilter
// (TraceFilter)). otelhttp v0.69.0 has no WithRouteTag helper (it was
// resolved by the module's semver-compatible latest, superseding older
// examples that used it); it instead derives the http.route attribute
// automatically from http.Request.Pattern, which net/http's ServeMux
// populates itself once it matches a route — see
// TestZZVerifyRouteAutoDetected. So there is only one otelhttp layer in
// production, not two, and this test exercises exactly that composition.
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

	rootHandler := otelhttp.NewHandler(mux, "mighty", otelhttp.WithFilter(TraceFilter))

	srv := httptest.NewServer(rootHandler)

	t.Cleanup(srv.Close)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/games/abc/ws"

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err, "upgrade failed through the production otelhttp.NewHandler chain")

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
