package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/redis/go-redis/v9"
)

// mapValidator resolves each token to a distinct user so one test can drive
// two sockets belonging to different players.
type mapValidator struct{ users map[string]string }

func (m *mapValidator) ValidateToken(_ context.Context, token string) (*service.AuthClaims, error) {
	id, ok := m.users[token]
	if !ok {
		return nil, service.ErrInvalidToken
	}

	return &service.AuthClaims{UserID: id, Username: id}, nil
}

// redactionRig is a game websocket server backed by a real (mini) Redis, so a
// test can publish an envelope and observe what each socket receives.
type redactionRig struct {
	server *httptest.Server
	client *redis.Client
	gameID string
}

func newRedactionRig(t *testing.T) *redactionRig {
	t.Helper()

	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	svc := &fakeWSGameService{redisClient: client, processMoveCh: make(chan struct{}, 1)}
	handler := NewHandler(svc, &mapValidator{users: map[string]string{
		"tok-0": "p0",
		"tok-1": "p1",
	}})

	mux := http.NewServeMux()
	mux.HandleFunc("/games/{id}/ws", handler.WSHandler)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &redactionRig{server: srv, client: client, gameID: "g1"}
}

// dial opens an authenticated socket for the given token.
func (r *redactionRig) dial(t *testing.T, token string) *websocket.Conn {
	t.Helper()

	url := "ws" + strings.TrimPrefix(r.server.URL, "http") + "/games/" + r.gameID + "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.WriteJSON(map[string]string{"type": "AUTH", "token": token}); err != nil {
		t.Fatalf("auth: %v", err)
	}

	return conn
}

func dealtGameForWS(t *testing.T) *game.Game {
	t.Helper()

	g := game.New("g1")
	for i := range 5 {
		g.Players[i] = &game.Player{ID: "p" + string(rune('0'+i)), Seat: i, IsConnected: true}
	}

	g.Start()

	return g
}

// publishWhenSubscribed retries the publish until miniredis reports the
// expected subscriber count, since Subscribe is asynchronous relative to the
// handler goroutine.
func (r *redactionRig) publishWhenSubscribed(t *testing.T, subscribers int64, payload []byte) {
	t.Helper()

	channel := "game:" + r.gameID + ":events"

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		n, err := r.client.Publish(t.Context(), channel, payload).Result()
		if err == nil && n >= subscribers {
			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("no publish reached %d subscribers within 3s", subscribers)
}

func readEvent(t *testing.T, conn *websocket.Conn) service.OutgoingGameEvent {
	t.Helper()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var ev service.OutgoingGameEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}

	return ev
}

func TestWSGivesEachSocketOnlyItsOwnHand(t *testing.T) {
	rig := newRedactionRig(t)
	conn0 := rig.dial(t, "tok-0")
	conn1 := rig.dial(t, "tok-1")

	payload, err := json.Marshal(service.GameEvent{
		Type:      service.EventTypeMove,
		MoveType:  game.MovePlayCard,
		Version:   2,
		GameState: dealtGameForWS(t),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rig.publishWhenSubscribed(t, 2, payload)

	for i, conn := range []*websocket.Conn{conn0, conn1} {
		ev := readEvent(t, conn)
		if ev.GameState == nil {
			t.Fatalf("socket %d received no game state", i)
		}

		if got := len(ev.GameState.Players[i].Hand); got != 10 {
			t.Errorf("socket %d should see its own 10 cards, got %d", i, got)
		}

		for seat, pv := range ev.GameState.Players {
			if seat != i && pv.Hand != nil {
				t.Errorf("socket %d saw seat %d's hand", i, seat)
			}
		}
	}
}

func TestWSDropsUnparseableEvents(t *testing.T) {
	rig := newRedactionRig(t)
	conn := rig.dial(t, "tok-0")

	rig.publishWhenSubscribed(t, 1, []byte(`{"type": "move", "game_state": "not-an-object"}`))

	good, err := json.Marshal(service.GameEvent{Type: service.EventTypeMove, Version: 9})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rig.publishWhenSubscribed(t, 1, good)

	// The malformed frame must never reach the socket, so the first readable
	// frame is the well-formed one that followed it.
	if ev := readEvent(t, conn); ev.Version != 9 {
		t.Errorf("expected the malformed frame to be dropped, got version %d", ev.Version)
	}
}
