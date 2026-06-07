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
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"golang.org/x/crypto/bcrypt"
)

const (
	keyUsername = "username"
	keyPassword = "password"
	keyEmail    = "email"
)

func setupAuthTestEnv(t *testing.T) (*Handler, sqlmock.Sqlmock, *sql.DB) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}

	pgStore := postgres.NewStoreWithDB(db)
	svc := service.NewGame(nil, pgStore) // redisStore is nil, ok for auth tests
	authSvc := service.NewAuth(pgStore, "testsecret")
	handler := NewHandler(svc, authSvc)

	return handler, mock, db
}

func TestSignupHandler_Success(t *testing.T) {
	t.Parallel()
	handler, mock, db := setupAuthTestEnv(t)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT id, username, password_hash, email, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs("newuser").
		WillReturnError(sql.ErrNoRows)

	mock.ExpectBegin()

	mock.ExpectExec(`INSERT INTO users`).
		WithArgs(sqlmock.AnyArg(), "newuser", sqlmock.AnyArg(), "new@example.com", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec(`INSERT INTO user_stats`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	payload := map[string]string{
		keyUsername: "newuser",
		keyPassword: "password123",
		keyEmail:    "new@example.com",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/signup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.SignupHandler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestSignupHandler_UserExists(t *testing.T) {
	t.Parallel()
	handler, mock, db := setupAuthTestEnv(t)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "username", "password_hash", "email", "created_at", "updated_at"}).
		AddRow("1", "existinguser", "hash", "existing@example.com", time.Now(), time.Now())

	mock.ExpectQuery(`SELECT id, username, password_hash, email, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs("existinguser").
		WillReturnRows(rows)

	payload := map[string]string{
		keyUsername: "existinguser",
		keyPassword: "password123",
		keyEmail:    "existing@example.com",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/signup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.SignupHandler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusConflict, rec.Code, rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestLoginHandler_Success(t *testing.T) {
	t.Parallel()
	handler, mock, db := setupAuthTestEnv(t)
	defer func() { _ = db.Close() }()

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)

	rows := sqlmock.NewRows([]string{"id", "username", "password_hash", "email", "created_at", "updated_at"}).
		AddRow("1", "testuser", string(hashedPassword), "test@example.com", time.Now(), time.Now())

	mock.ExpectQuery(`SELECT id, username, password_hash, email, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs("testuser").
		WillReturnRows(rows)

	payload := map[string]string{
		keyUsername: "testuser",
		keyPassword: "password123",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.LoginHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["token"] == "" {
		t.Errorf("expected non-empty token")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestLoginHandler_InvalidCredentials(t *testing.T) {
	t.Parallel()
	handler, mock, db := setupAuthTestEnv(t)
	defer func() { _ = db.Close() }()

	hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)

	rows := sqlmock.NewRows([]string{"id", "username", "password_hash", "email", "created_at", "updated_at"}).
		AddRow("1", "testuser", string(hashedPassword), "test@example.com", time.Now(), time.Now())

	mock.ExpectQuery(`SELECT id, username, password_hash, email, created_at, updated_at FROM users WHERE username = \$1`).
		WithArgs("testuser").
		WillReturnRows(rows)

	payload := map[string]string{
		keyUsername: "testuser",
		keyPassword: "wrongpassword",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	handler.LoginHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusUnauthorized, rec.Code, rec.Body.String())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}
