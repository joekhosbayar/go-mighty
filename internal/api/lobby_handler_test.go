package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"github.com/redis/go-redis/v9"
)

// fakeValidator is a shared TokenValidator test double used across the api
// package tests in place of a real service.CognitoAuth. A nil claims field
// makes every token (including non-empty ones) fail validation, matching
// the previous "invalid token" behavior; a non-nil claims field authenticates
// any non-empty token as that identity.
type fakeValidator struct{ claims *service.AuthClaims }

func (f *fakeValidator) ValidateToken(_ context.Context, token string) (*service.AuthClaims, error) {
	if token == "" || f.claims == nil {
		return nil, service.ErrInvalidToken
	}

	return f.claims, nil
}

const (
	testGameID = "game-123"
)

type fakeRedisStore struct {
	mu    sync.RWMutex
	games map[string]*game.Game
}

func (f *fakeRedisStore) SaveGame(_ context.Context, _ *game.Game, _ int64) error { return nil }
func (f *fakeRedisStore) LoadGame(_ context.Context, gameID string) (*game.Game, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if g, ok := f.games[gameID]; ok {
		return g, nil
	}

	return nil, nil
}

func (f *fakeRedisStore) AcquireLock(_ context.Context, _ string) (string, error) {
	return "test-token", nil
}
func (f *fakeRedisStore) ReleaseLock(_ context.Context, _, _ string) error { return nil }

func (f *fakeRedisStore) PublishEvent(_ context.Context, _ string, _ any) error {
	return nil
}
func (f *fakeRedisStore) Subscribe(_ context.Context, _ string) *redis.PubSub { return nil }

func setupLobbyTestEnv(t *testing.T) (*Handler, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	return setupLobbyTestEnvWithRedis(t, &fakeRedisStore{games: map[string]*game.Game{}})
}

func setupLobbyTestEnvWithRedis(t *testing.T, redisStore service.RedisStore) (*Handler, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}

	pgStore := postgres.NewStoreWithDB(db)
	svc := service.NewGame(redisStore, pgStore)
	authSvc := &fakeValidator{claims: &service.AuthClaims{UserID: "player-1", Username: "alice"}}
	handler := NewHandler(svc, authSvc)

	return handler, mock, db
}

// generateValidToken returns a placeholder bearer token string. Its content
// is irrelevant under fakeValidator — only its identity (the caller's
// userID/username) matters, and that comes from the handler's fakeValidator,
// not the token itself. Non-empty is all that's required for a successful
// authentication.
func generateValidToken(_, _ string) string {
	return "valid-test-token"
}

func TestListGamesHandler_Success(t *testing.T) {
	t.Parallel()
	redisStore := &fakeRedisStore{
		games: map[string]*game.Game{
			testGameID: {ID: testGameID, Status: game.PhaseWaiting},
			"game-456": {ID: "game-456", Status: game.PhaseWaiting},
		},
	}

	handler, mock, db := setupLobbyTestEnvWithRedis(t, redisStore)
	defer func() { _ = db.Close() }()

	// Mock the postgres query for waiting games
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow(testGameID).
		AddRow("game-456")

	mock.ExpectQuery(`SELECT id FROM games WHERE status = \$1 ORDER BY created_at DESC LIMIT 50`).
		WithArgs("waiting").
		WillReturnRows(rows)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games?status=waiting", nil)
	rec := httptest.NewRecorder()

	handler.ListGamesHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp []*game.Game
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 games, got %d", len(resp))
	}

	if resp[0].ID != testGameID || resp[1].ID != "game-456" {
		t.Fatalf("unexpected game ids in response: %+v", resp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestJoinGameHandler_Unauthorized_NoToken(t *testing.T) {
	t.Parallel()
	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()

	payload := map[string]any{
		"seat": 0,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games/"+testGameID+"/join", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.JoinGameHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestListGamesHandler_InvalidStatus(t *testing.T) {
	t.Parallel()
	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games?status=unknown", nil)
	rec := httptest.NewRecorder()

	handler.ListGamesHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestJoinGameHandler_Unauthorized_InvalidToken(t *testing.T) {
	t.Parallel()
	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()
	handler.authSvc = &fakeValidator{}

	payload := map[string]any{
		"seat": 0,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games/"+testGameID+"/join", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid.token.string")

	rec := httptest.NewRecorder()

	handler.JoinGameHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestJoinGameHandler_Unauthorized_QueryToken(t *testing.T) {
	t.Parallel()
	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()

	payload := map[string]any{
		"seat": 0,
	}
	body, _ := json.Marshal(payload)

	token := generateValidToken("player-1", "alice")
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games/"+testGameID+"/join?token="+token, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.JoinGameHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestJoinGameHandler_GameNotFound(t *testing.T) {
	t.Parallel()
	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()

	token := generateValidToken("player-1", "alice")
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games/missing/join", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", "missing")

	rec := httptest.NewRecorder()

	handler.JoinGameHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusNotFound, rec.Code, rec.Body.String())
	}
}

func TestJoinGameHandler_GameFull(t *testing.T) {
	t.Parallel()
	fullGame := game.New(testGameID)
	for i := range len(fullGame.Players) {
		fullGame.Players[i] = &game.Player{ID: fmt.Sprintf("player-%d", i+1), Name: "player", Seat: i}
	}

	redisStore := &fakeRedisStore{
		games: map[string]*game.Game{
			testGameID: fullGame,
		},
	}

	handler, _, db := setupLobbyTestEnvWithRedis(t, redisStore)
	defer func() { _ = db.Close() }()
	handler.authSvc = &fakeValidator{claims: &service.AuthClaims{UserID: "player-new", Username: "alice"}}

	token := generateValidToken("player-new", "alice")
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games/"+testGameID+"/join", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("id", testGameID)

	rec := httptest.NewRecorder()

	handler.JoinGameHandler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}
}

func TestMoveHandler_Unauthorized_NoToken(t *testing.T) {
	t.Parallel()
	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()

	body := []byte(`{"player_id":"player-1","move_type":"pass","client_version":0,"payload":null}`)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games/"+testGameID+"/move", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", testGameID)

	rec := httptest.NewRecorder()

	handler.MoveHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestMoveHandler_Unauthorized_InvalidToken(t *testing.T) {
	t.Parallel()
	handler, _, db := setupLobbyTestEnv(t)
	defer func() { _ = db.Close() }()
	handler.authSvc = &fakeValidator{}

	body := []byte(`{"player_id":"player-1","move_type":"pass","client_version":0,"payload":null}`)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games/"+testGameID+"/move", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid.token.string")
	req.SetPathValue("id", testGameID)

	rec := httptest.NewRecorder()

	handler.MoveHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}
