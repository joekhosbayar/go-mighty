package infra

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/lib/pq"

	"github.com/rs/zerolog/log"
)

// ----------------------------
// Interfaces
// ----------------------------

type Row interface {
	Scan(dest ...any) error
}

type Database interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) Row
	PingContext(ctx context.Context) error
}

// ----------------------------
// Postgres client
// ----------------------------

type PostgresClient struct {
	db Database
}

func (p *PostgresClient) Close() error {
	if p.db == nil {
		return nil
	}

	if closer, ok := p.db.(interface{ Close() error }); ok && closer != nil {
		return closer.Close()
	}

	return nil
}
func (p *PostgresClient) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

func (p *PostgresClient) Exec(ctx context.Context, query string, args ...any) error {
	_, err := p.db.ExecContext(ctx, query, args...)
	return err
}

func (p *PostgresClient) QueryRow(ctx context.Context, query string, args ...any) Row {
	return p.db.QueryRowContext(ctx, query, args...)
}

// ----------------------------
// Real DB adapter
// ----------------------------

type realDatabase struct {
	conn *sql.DB
}

func (r *realDatabase) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return r.conn.ExecContext(ctx, query, args...)
}

func (r *realDatabase) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.conn.QueryContext(ctx, query, args...)
}

func (r *realDatabase) QueryRowContext(ctx context.Context, query string, args ...any) Row {
	return &sqlRowAdapter{row: r.conn.QueryRowContext(ctx, query, args...)}
}

func (r *realDatabase) PingContext(ctx context.Context) error {
	return r.conn.PingContext(ctx)
}

// Adapter for *sql.Row to implement Row interface
type sqlRowAdapter struct {
	row *sql.Row
}

func (s *sqlRowAdapter) Scan(dest ...any) error {
	return s.row.Scan(dest...)
}

// ----------------------------
// Constructor
// ----------------------------

func ProvidePostgresClient() *PostgresClient {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "postgres"
	}

	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}

	user := os.Getenv("POSTGRES_USER")
	if user == "" {
		user = "postgres"
	}

	password := os.Getenv("POSTGRES_PASSWORD")
	dbName := os.Getenv("POSTGRES_DB")
	if dbName == "" {
		dbName = "postgres"
	}

	sslMode := os.Getenv("POSTGRES_SSLMODE")
	if sslMode == "" {
		sslMode = "disable"
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbName, sslMode,
	)
	safeDSN := fmt.Sprintf(
		"host=%s port=%s user=%s dbname=%s sslmode=%s",
		host, port, user, dbName, sslMode,
	)
	log.Info().Msg(safeDSN)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open postgres connection")
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &PostgresClient{
		db: &realDatabase{conn: db},
	}
}
