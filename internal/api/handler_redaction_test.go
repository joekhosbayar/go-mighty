package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joekhosbayar/go-mighty/internal/game"
)

func dealtGameForREST() *game.Game {
	g := game.New(testGameID)
	for i := range 5 {
		g.Players[i] = &game.Player{ID: "player-" + string(rune('1'+i)), Seat: i, IsConnected: true}
	}

	g.Start()

	return g
}

func TestGetGameHandlerRequiresAuth(t *testing.T) {
	t.Parallel()

	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games/"+testGameID, nil)
	req.SetPathValue("id", testGameID)

	rec := httptest.NewRecorder()
	handler.GetGameHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without a token, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestGetGameHandlerRedactsOtherHands(t *testing.T) {
	t.Parallel()

	// setupLobbyTestEnv's fakeValidator authenticates every non-empty token as
	// player-1, who holds seat 0.
	redisStore := &fakeRedisStore{games: map[string]*game.Game{testGameID: dealtGameForREST()}}

	handler, _, db := setupLobbyTestEnvWithRedis(t, redisStore)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games/"+testGameID, nil)
	req.SetPathValue("id", testGameID)
	req.Header.Set("Authorization", "Bearer "+generateValidToken("player-1", "alice"))

	rec := httptest.NewRecorder()
	handler.GetGameHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Players []*game.PlayerView `json:"players"`
		Kitty   []game.Card        `json:"kitty"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got := len(resp.Players[0].Hand); got != 10 {
		t.Errorf("caller should see their own 10 cards, got %d", got)
	}

	for seat := 1; seat < 5; seat++ {
		if resp.Players[seat].Hand != nil {
			t.Errorf("seat %d's hand leaked over REST", seat)
		}

		if resp.Players[seat].HandCount != 10 {
			t.Errorf("seat %d: want hand_count 10, got %d", seat, resp.Players[seat].HandCount)
		}
	}

	if resp.Kitty != nil {
		t.Errorf("kitty leaked over REST: %v", resp.Kitty)
	}
}

func TestListGamesHandlerRequiresAuth(t *testing.T) {
	t.Parallel()

	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games?status=waiting", nil)
	rec := httptest.NewRecorder()

	handler.ListGamesHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without a token, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}
