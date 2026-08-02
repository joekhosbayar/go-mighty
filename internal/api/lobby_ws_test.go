package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/service"
)

func setupLobbyWSTestServer(t *testing.T) (*httptest.Server, *fakeWSGameService) {
	t.Helper()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)

	svc, ok := handler.svc.(*fakeWSGameService)
	if !ok {
		t.Fatal("expected fake websocket game service")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /lobby/ws", handler.LobbyWSHandler)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server, svc
}

func TestLobbyWSHandler_Success(t *testing.T) {
	t.Parallel()

	server, svc := setupLobbyWSTestServer(t)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/lobby/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to lobby ws: %v", err)
	}
	defer conn.Close()

	// Send AUTH message
	authMsg := map[string]string{
		"type":  "AUTH",
		"token": "valid_token",
	}
	authBytes, _ := json.Marshal(authMsg)
	if err := conn.WriteMessage(websocket.TextMessage, authBytes); err != nil {
		t.Fatalf("failed to send auth message: %v", err)
	}

	// Wait briefly for subscription to settle
	time.Sleep(50 * time.Millisecond)

	// open_games is not declared on service.LobbyEvent. Publishing it here
	// pins the fail-closed property: the relay decodes into the envelope type
	// and re-encodes, so an undeclared field is dropped rather than forwarded.
	eventPayload := `{"type":"game_joined","game_id":"g1","players_seated":2,"max_players":5,"open_games":3}`

	err = svc.redisClient.Publish(t.Context(), "game:lobby_events:events", eventPayload).Err()
	if err != nil {
		t.Fatalf("failed to publish lobby event: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message from ws: %v", err)
	}

	var got service.LobbyEvent
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("failed to decode relayed lobby event %s: %v", msg, err)
	}

	if got.Type != service.EventTypeGameJoined || got.GameID != "g1" || got.PlayersSeated != 2 || got.MaxPlayers != 5 {
		t.Errorf("relayed lobby event mangled: %+v", got)
	}

	if strings.Contains(string(msg), "open_games") {
		t.Errorf("undeclared field survived the relay: %s", msg)
	}
}

func TestLobbyWSHandler_InvalidAuthMessage(t *testing.T) {
	t.Parallel()

	server, _ := setupLobbyWSTestServer(t)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/lobby/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to lobby ws: %v", err)
	}
	defer conn.Close()

	invalidMsg := map[string]string{
		"type": "NOT_AUTH",
	}
	msgBytes, _ := json.Marshal(invalidMsg)
	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		t.Fatalf("failed to send invalid auth message: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read error message: %v", err)
	}

	var errResp OutgoingWSError
	if err := json.Unmarshal(msg, &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Error != "expected AUTH message" {
		t.Errorf("expected 'expected AUTH message', got %q", errResp.Error)
	}
}

func TestLobbyWSHandler_UnauthorizedToken(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	handler.authSvc = &fakeValidator{claims: nil} // nil claims => unauthorized

	mux := http.NewServeMux()
	mux.HandleFunc("GET /lobby/ws", handler.LobbyWSHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/lobby/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to lobby ws: %v", err)
	}
	defer conn.Close()

	authMsg := map[string]string{
		"type":  "AUTH",
		"token": "invalid_token",
	}
	authBytes, _ := json.Marshal(authMsg)
	_ = conn.WriteMessage(websocket.TextMessage, authBytes)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read error message: %v", err)
	}

	var errResp OutgoingWSError
	if err := json.Unmarshal(msg, &errResp); err != nil {
		t.Fatalf("failed to unmarshal error response: %v", err)
	}

	if errResp.Error != "unauthorized" {
		t.Errorf("expected 'unauthorized', got %q", errResp.Error)
	}
}
