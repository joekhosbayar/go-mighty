package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
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
