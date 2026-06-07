// Package infra provides infrastructure-level components like database and cache clients.
package infra

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq" // Import the postgres driver for database/sql.

	"github.com/rs/zerolog/log"
)

// ----------------------------
// Interfaces
// ----------------------------

// Row is an interface that abstracts sql.Row and sql.Rows for scanning.
type Row interface {
	Scan(dest ...any) error
}

// Database is an interface that abstracts sql.DB for testing and flexibility.
type Database interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) Row
	PingContext(ctx context.Context) error
}

// ----------------------------
// Postgres client
// ----------------------------

// Postgres is a wrapper around the Database interface providing common PostgreSQL operations.
type Postgres struct {
	db Database
}

// Close closes the underlying database connection.
func (p *Postgres) Close() error {
	if p.db == nil {
		return nil
	}

	if closer, ok := p.db.(interface{ Close() error }); ok && closer != nil {
		return closer.Close()
	}

	return nil
}

// Ping checks if the database connection is alive.
func (p *Postgres) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

// Exec executes a query without returning any rows.
func (p *Postgres) Exec(ctx context.Context, query string, args ...any) error {
	_, err := p.db.ExecContext(ctx, query, args...)
	return err
}

// QueryRow executes a query that is expected to return at most one row.
func (p *Postgres) QueryRow(ctx context.Context, query string, args ...any) Row {
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

// Adapter for *sql.Row to implement Row interface.
type sqlRowAdapter struct {
	row *sql.Row
}

func (s *sqlRowAdapter) Scan(dest ...any) error {
	return s.row.Scan(dest...)
}

// readSecret reads a secret from Docker secrets file or falls back to environment variable.
func readSecret(secretName, envVarName string) string {
	// Try to read from Docker secret file first
	secretPath := "/run/secrets/" + secretName

	data, err := os.ReadFile(secretPath)
	if err == nil {
		log.Debug().Str("secret", secretName).Msg("Successfully read secret from Docker secrets file")
		return strings.TrimSpace(string(data))
	}

	// Log the reason for fallback (but don't log the actual error details for security)
	log.Debug().Str("secret", secretName).Msg("Docker secret not found, using environment variable")

	// Fall back to environment variable
	return os.Getenv(envVarName)
}

// ----------------------------
// Constructor
// ----------------------------

// NewPostgres creates and returns a new Postgres client instance.
func NewPostgres() *Postgres {
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

	password := readSecret("postgres_password", "POSTGRES_PASSWORD")

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

	return &Postgres{
		db: &realDatabase{conn: db},
	}
}
