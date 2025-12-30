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

type PostgresClient struct {
	db Database
}

type Database interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	PingContext(ctx context.Context) error
}

func (p *PostgresClient) Ping(ctx context.Context) error {
	return p.db.PingContext(ctx)
}

func (p *PostgresClient) Exec(
	ctx context.Context,
	query string,
	args ...any,
) error {
	_, err := p.db.ExecContext(ctx, query, args...)
	return err
}

func (p *PostgresClient) QueryRow(
	ctx context.Context,
	query string,
	args ...any,
) *sql.Row {
	return p.db.QueryRowContext(ctx, query, args...)
}

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
		host,
		port,
		user,
		password,
		dbName,
		sslMode,
	)
	log.Info().Msg(dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open postgres connection")
	}

	// Sensible pool defaults
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &PostgresClient{
		db: db,
	}
}
