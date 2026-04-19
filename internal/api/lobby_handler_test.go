package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
)

func setupLobbyTestEnv(t *testing.T) (*Handler, sqlmock.Sqlmock, *sql.DB) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}

	pgStore := postgres.NewStoreWithDB(db)
	svc := service.NewGameService(nil, pgStore) // redisStore is nil, returns empty games
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
	handler, mock, db := setupLobbyTestEnv(t)
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

	var resp []interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Since redisStore is nil, it returns an empty slice of games rather than panicking
	if len(resp) != 0 {
		t.Errorf("expected empty games array (mocked), got %d games", len(resp))
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
