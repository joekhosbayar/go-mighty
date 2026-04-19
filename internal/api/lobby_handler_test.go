package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"github.com/redis/go-redis/v9"
)

type fakeRedisStore struct {
	games map[string]*game.GameState
}

func (f *fakeRedisStore) SaveGame(ctx context.Context, g *game.GameState) error { return nil }
func (f *fakeRedisStore) LoadGame(ctx context.Context, gameID string) (*game.GameState, error) {
	if g, ok := f.games[gameID]; ok {
		return g, nil
	}
	return nil, nil
}
func (f *fakeRedisStore) AcquireLock(ctx context.Context, gameID string) (bool, error) {
	return true, nil
}
func (f *fakeRedisStore) ReleaseLock(ctx context.Context, gameID string) error { return nil }
func (f *fakeRedisStore) CheckVersion(ctx context.Context, gameID string, clientVersion int64) error {
	return nil
}
func (f *fakeRedisStore) PublishEvent(ctx context.Context, gameID string, event interface{}) error {
	return nil
}
func (f *fakeRedisStore) Subscribe(ctx context.Context, gameID string) *redis.PubSub { return nil }

func setupLobbyTestEnv(t *testing.T) (*Handler, sqlmock.Sqlmock, *sql.DB) {
	return setupLobbyTestEnvWithRedis(t, &fakeRedisStore{games: map[string]*game.GameState{}})
}

func setupLobbyTestEnvWithRedis(t *testing.T, redisStore service.RedisStore) (*Handler, sqlmock.Sqlmock, *sql.DB) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}

	pgStore := postgres.NewStoreWithDB(db)
	svc := service.NewGameService(redisStore, pgStore)
	authSvc := service.NewAuthService(pgStore, "testsecret")
	handler := NewHandler(svc, authSvc)

	return handler, mock, db
}

func generateValidToken(userID, username string) string {
	claims := &service.AuthClaims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, _ := token.SignedString([]byte("testsecret"))
	return signedToken
}

func TestListGamesHandler_Success(t *testing.T) {
	redisStore := &fakeRedisStore{
		games: map[string]*game.GameState{
			"game-123": {ID: "game-123", Status: game.PhaseWaiting},
			"game-456": {ID: "game-456", Status: game.PhaseWaiting},
		},
	}
	handler, mock, db := setupLobbyTestEnvWithRedis(t, redisStore)
	defer db.Close()

	// Mock the postgres query for waiting games
	rows := sqlmock.NewRows([]string{"id"}).
		AddRow("game-123").
		AddRow("game-456")

	mock.ExpectQuery(`SELECT id FROM games WHERE status = \$1 ORDER BY created_at DESC LIMIT 50`).
		WithArgs("waiting").
		WillReturnRows(rows)

	req := httptest.NewRequest(http.MethodGet, "/games?status=waiting", nil)
	rec := httptest.NewRecorder()

	handler.ListGamesHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp []*game.GameState
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 games, got %d", len(resp))
	}
	if resp[0].ID != "game-123" || resp[1].ID != "game-456" {
		t.Fatalf("unexpected game ids in response: %+v", resp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestJoinGameHandler_Unauthorized_NoToken(t *testing.T) {
	handler, _, db := setupLobbyTestEnv(t)
	defer db.Close()

	payload := map[string]interface{}{
		"seat": 0,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/games/game-123/join", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.JoinGameHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}

func TestListGamesHandler_InvalidStatus(t *testing.T) {
	handler, _, db := setupLobbyTestEnv(t)
	defer db.Close()

	req := httptest.NewRequest(http.MethodGet, "/games?status=unknown", nil)
	rec := httptest.NewRecorder()

	handler.ListGamesHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusBadRequest, rec.Code, rec.Body.String())
	}
}

func TestJoinGameHandler_Unauthorized_InvalidToken(t *testing.T) {
	handler, _, db := setupLobbyTestEnv(t)
	defer db.Close()

	payload := map[string]interface{}{
		"seat": 0,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/games/game-123/join", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer invalid.token.string")
	rec := httptest.NewRecorder()

	handler.JoinGameHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
}
