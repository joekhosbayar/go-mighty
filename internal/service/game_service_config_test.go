package service

import (
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
)

// newTestServiceWithConfig mirrors the redis-only setup used throughout this
// file, adding a sqlmock-backed postgres store since CreateGame persists to
// both stores.
func newTestServiceWithConfig(t *testing.T) (*Game, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("an error '%s' was not expected when opening a stub database connection", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	pgStore := postgres.NewStoreWithDB(db)
	mock.ExpectExec(`INSERT INTO games \(id, status, version, created_at\) VALUES \(\$1, \$2, \$3, \$4\)`).
		WillReturnResult(sqlmock.NewResult(1, 1))

	redisStore := &fakeRedisStore{}
	svc := &Game{redisStore: redisStore, postgresStore: pgStore}

	return svc, mock
}

func TestCreateGameStoresFourPlayerConfig(t *testing.T) {
	t.Parallel()

	svc, mock := newTestServiceWithConfig(t)
	cfg := game.GameConfig{NumPlayers: 4, AllowJokerPartner: false, FailDist: game.FailTwoOneSplit}

	g, err := svc.CreateGame(t.Context(), "cfg-game", cfg)
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	if g.Config.NumPlayers != 4 || g.Config.AllowJokerPartner || g.Config.FailDist != game.FailTwoOneSplit {
		t.Fatalf("config not stored: %+v", g.Config)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet postgres expectations: %v", err)
	}
}
