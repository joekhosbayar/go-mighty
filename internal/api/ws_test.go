package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"github.com/redis/go-redis/v9"
)

const (
	keyType     = "type"
	keyMoveType = "move_type"
)

type fakeWSGameService struct {
	mu                sync.Mutex
	redisClient       *redis.Client
	processMoveCalled bool
	processMoveCh     chan struct{}
	processMoveErr    error
}

func (f *fakeWSGameService) CreateGame(_ context.Context, _ string, _ game.GameConfig) (*game.Game, error) {
	return nil, nil
}

func (f *fakeWSGameService) JoinGame(_ context.Context, _, _, _ string) (*game.Game, error) {
	return nil, nil
}

func (f *fakeWSGameService) ProcessMove(ctx context.Context, gameID, playerID string, moveType game.MoveType, _ any, clientVersion int64) (*game.Game, error) {
	f.mu.Lock()
	f.processMoveCalled = true
	f.mu.Unlock()

	select {
	case f.processMoveCh <- struct{}{}:
	default:
	}

	if f.processMoveErr != nil {
		return nil, f.processMoveErr
	}

	event := map[string]any{
		keyType:      "move",
		keyMoveType:  moveType,
		"player_id":  playerID,
		"version":    clientVersion + 1,
		"game_state": map[string]string{"id": gameID},
	}

	data, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	if err := f.redisClient.Publish(ctx, "game:"+gameID+":events", data).Err(); err != nil {
		return nil, err
	}

	return &game.Game{ID: gameID}, nil
}

func (f *fakeWSGameService) Subscribe(ctx context.Context, gameID string) *redis.PubSub {
	return f.redisClient.Subscribe(ctx, "game:"+gameID+":events")
}

func (_ *fakeWSGameService) GetGame(_ context.Context, _ string) (*game.Game, error) {
	return nil, nil
}

func (_ *fakeWSGameService) ListGamesByStatus(_ context.Context, _ game.Phase) ([]*game.Game, error) {
	return nil, nil
}

func (f *fakeWSGameService) WasProcessMoveCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.processMoveCalled
}

func (f *fakeWSGameService) WaitForProcessMove(timeout time.Duration) bool {
	select {
	case <-f.processMoveCh:
		return true
	case <-time.After(timeout):
		return false
	}
}

func setupWSTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})

	svc := &fakeWSGameService{
		redisClient:   client,
		processMoveCh: make(chan struct{}, 1),
	}
	authSvc := service.NewAuth(&postgres.Store{}, "testsecret")
	handler := NewHandler(svc, authSvc)

	cleanup := func() {
		_ = client.Close()
		mini.Close()
	}

	return handler, cleanup
}

func setupWSTestServer(t *testing.T) (*httptest.Server, *fakeWSGameService) {
	t.Helper()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)

	svc, ok := handler.svc.(*fakeWSGameService)
	if !ok {
		t.Fatal("expected fake websocket game service")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/games/{id}/ws", handler.WSHandler)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server, svc
}

// TestWSHandler_RejectEarlyData confirms that the server rejects a WebSocket handshake
// if the client sends data before the handshake is complete.
func TestWSHandler_RejectEarlyData(t *testing.T) {
	t.Parallel()
	server, _ := setupWSTestServer(t)

	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("Failed to split host/port: %v", err)
	}

	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp", "localhost:"+port)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	req := "GET /games/test-game/ws HTTP/1.1\r\n" +
		"Host: localhost:" + port + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Origin: http://localhost:" + port + "\r\n" +
		"\r\n"

	extraData := []byte{0x81, 0x05, 'h', 'e', 'l', 'l', 'o'}

	if _, err = conn.Write(append([]byte(req), extraData...)); err != nil {
		t.Fatalf("Failed to write request with early data: %v", err)
	}

	reader := bufio.NewReader(conn)

	resp, err := http.ReadResponse(reader, nil)
	if err == nil {
		if resp.StatusCode == http.StatusSwitchingProtocols {
			t.Errorf("Expected handshake failure, got 101 Switching Protocols")
		}

		_ = resp.Body.Close()
	}
}

func dialWS(t *testing.T, server *httptest.Server, path, token string) *websocketConn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + path

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if err != nil {
		if resp != nil {
			t.Fatalf("websocket dial failed: %v (status=%d)", err, resp.StatusCode)
		}

		t.Fatalf("websocket dial failed: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	if token != "" {
		authMsg := map[string]string{
			keyType: "AUTH",
			"token": token,
		}
		if err := conn.WriteJSON(authMsg); err != nil {
			t.Fatalf("failed to send auth message: %v", err)
		}
	}

	return &websocketConn{Conn: conn}
}

func TestWSHandler_InvalidJSONReturnsErrorFrame(t *testing.T) {
	t.Parallel()
	server, _ := setupWSTestServer(t)
	token := generateValidToken("user-1", "alice")
	conn := dialWS(t, server, "/games/game-1/ws", token)

	if err := conn.WriteText("not-json"); err != nil {
		t.Fatalf("failed to write invalid json: %v", err)
	}

	msg := conn.ReadText(t)
	if msg.Type != WSMessageTypeError || !strings.Contains(msg.Error, "invalid message format") {
		t.Fatalf("unexpected ws error response: %+v", msg)
	}
}

func TestWSHandler_FirstMessageMustBeAuth(t *testing.T) {
	t.Parallel()
	server, _ := setupWSTestServer(t)
	conn := dialWS(t, server, "/games/game-1/ws", "")

	if err := conn.WriteJSON(map[string]any{
		keyType:          WSMessageTypeMove,
		keyMoveType:      "pass",
		"payload":        nil,
		"client_version": 1,
	}); err != nil {
		t.Fatalf("failed to write non-auth first message: %v", err)
	}

	msg := conn.ReadText(t)
	if msg.Type != WSMessageTypeError || !strings.Contains(msg.Error, "expected AUTH message") {
		t.Fatalf("unexpected ws error response: %+v", msg)
	}
}

func TestWSHandler_InvalidMovePayloadReturnsErrorFrame(t *testing.T) {
	t.Parallel()
	server, _ := setupWSTestServer(t)
	token := generateValidToken("user-1", "alice")
	conn := dialWS(t, server, "/games/game-1/ws", token)

	if err := conn.WriteJSON(map[string]any{
		keyType:          WSMessageTypeMove,
		keyMoveType:      "bid",
		"payload":        "bad-payload",
		"client_version": 1,
	}); err != nil {
		t.Fatalf("failed to write invalid move payload: %v", err)
	}

	msg := conn.ReadText(t)
	if msg.Type != WSMessageTypeError || !strings.Contains(msg.Error, "invalid payload structure") {
		t.Fatalf("unexpected ws error response: %+v", msg)
	}
}

func TestWSHandler_ValidMoveCallsProcessMoveAndForwardsEvent(t *testing.T) {
	t.Parallel()
	server, svc := setupWSTestServer(t)
	token := generateValidToken("user-2", "bob")
	conn := dialWS(t, server, "/games/game-1/ws", token)

	if err := conn.WriteJSON(map[string]any{
		keyType:          WSMessageTypeMove,
		keyMoveType:      "pass",
		"payload":        nil,
		"client_version": 3,
	}); err != nil {
		t.Fatalf("failed to write valid move: %v", err)
	}

	if !svc.WaitForProcessMove(2*time.Second) || !svc.WasProcessMoveCalled() {
		t.Fatal("expected ProcessMove to be called")
	}

	msg := conn.ReadRawText(t)
	if !strings.Contains(msg, `"type":"move"`) {
		t.Fatalf("expected forwarded move event, got: %s", msg)
	}
}

type wsErrorMessage struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

type websocketConn struct {
	Conn *websocket.Conn
}

func (c *websocketConn) WriteText(msg string) error {
	return c.Conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

func (c *websocketConn) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return c.Conn.WriteMessage(websocket.TextMessage, data)
}

func (c *websocketConn) ReadText(t *testing.T) wsErrorMessage {
	t.Helper()

	if err := c.setReadDeadline(2 * time.Second); err != nil {
		t.Fatalf("failed to set websocket read deadline: %v", err)
	}

	_, data, err := c.Conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read websocket message: %v", err)
	}

	var msg wsErrorMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("failed to decode websocket error message: %v (%s)", err, string(data))
	}

	return msg
}

func (c *websocketConn) ReadRawText(t *testing.T) string {
	t.Helper()

	if err := c.setReadDeadline(2 * time.Second); err != nil {
		t.Fatalf("failed to set websocket read deadline: %v", err)
	}

	_, data, err := c.Conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read websocket message: %v", err)
	}

	return string(data)
}

func (c *websocketConn) setReadDeadline(timeout time.Duration) error {
	return c.Conn.SetReadDeadline(time.Now().Add(timeout))
}
