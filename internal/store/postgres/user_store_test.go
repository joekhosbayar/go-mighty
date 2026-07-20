package postgres

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func testTime() time.Time { return time.Unix(0, 0) }

func TestUpsertUserByCognitoSub_CreatesOnFirstSight(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Store{db: db}

	// Fast-path lookup misses first (the upsert SELECTs before opening a tx).
	empty := sqlmock.NewRows([]string{"id", "username", "cognito_sub", "email", "created_at", "updated_at"})
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, cognito_sub`)).
		WithArgs("sub-123").WillReturnRows(empty)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO users`)).
		WithArgs("sub-123", "alice", "sub-123").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO user_stats`)).
		WithArgs("sub-123").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	rows := sqlmock.NewRows([]string{"id", "username", "cognito_sub", "email", "created_at", "updated_at"}).
		AddRow("sub-123", "alice", "sub-123", nil, testTime(), testTime())
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, cognito_sub`)).
		WithArgs("sub-123").WillReturnRows(rows)

	user, err := s.UpsertUserByCognitoSub(context.Background(), "sub-123", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != "sub-123" || user.Username != "alice" {
		t.Fatalf("got %+v", user)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGetUserByCognitoSub_NotFoundReturnsNil(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Store{db: db}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, username, cognito_sub`)).
		WithArgs("nope").WillReturnRows(sqlmock.NewRows([]string{"id", "username", "cognito_sub", "email", "created_at", "updated_at"}))

	user, err := s.GetUserByCognitoSub(context.Background(), "nope")
	if err != nil || user != nil {
		t.Fatalf("want nil,nil got %v,%v", user, err)
	}
}
