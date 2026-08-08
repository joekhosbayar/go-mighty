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

// TestLobbyWSHandler_HandAndKittyDoNotSurviveTheRelay pins the Fix-1
// property: even if a future publisher builds a LobbyEvent from a raw
// game.Game (or otherwise smuggles hand/kitty keys onto the wire before
// they reach Redis), game.LobbyGameView has no field capable of holding
// them, so json.Unmarshal into service.LobbyEvent silently drops the keys
// and json.Marshal never re-emits them. This is what "structurally cannot
// carry secrets" buys over the runtime-scrubbing alternative.
func TestLobbyWSHandler_HandAndKittyDoNotSurviveTheRelay(t *testing.T) {
	t.Parallel()

	server, svc := setupLobbyWSTestServer(t)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/lobby/ws"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to lobby ws: %v", err)
	}
	defer func() { _ = conn.Close() }()

	authMsg := map[string]string{"type": "AUTH", "token": "valid_token"}

	authBytes, _ := json.Marshal(authMsg)
	if err := conn.WriteMessage(websocket.TextMessage, authBytes); err != nil {
		t.Fatalf("failed to send auth message: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// A hostile or buggy publisher: a full hand on seat 0 and a populated
	// kitty riding along on a game_created event. Neither field exists on
	// game.LobbyGameView, so this proves the relay drops them rather than
	// merely trusting that no publisher will ever send them.
	eventPayload := `{"type":"game_created","game":{` +
		`"id":"g1","status":"waiting","config":{"num_players":5},` +
		`"players":[{"id":"p0","name":"P0","seat":0,` +
		`"hand":[{"suit":"spades","rank":"A"}],"hand_count":10,"is_connected":true}],` +
		`"kitty":[{"suit":"hearts","rank":"K"}],` +
		`"version":1,"created_at":"2026-01-01T00:00:00Z"}}`

	err = svc.redisClient.Publish(t.Context(), "game:lobby_events:events", eventPayload).Err()
	if err != nil {
		t.Fatalf("failed to publish lobby event: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message from ws: %v", err)
	}

	if strings.Contains(string(msg), `"hand"`) {
		t.Errorf("a hand reached the lobby socket: %s", msg)
	}

	if strings.Contains(string(msg), `"kitty"`) {
		t.Errorf("the kitty reached the lobby socket: %s", msg)
	}

	// The rest of the row should still make it through intact.
	var got service.LobbyEvent
	if err := json.Unmarshal(msg, &got); err != nil {
		t.Fatalf("failed to decode relayed lobby event %s: %v", msg, err)
	}

	if got.Game == nil || got.Game.ID != "g1" {
		t.Errorf("expected game g1 to survive the relay, got %+v", got.Game)
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
