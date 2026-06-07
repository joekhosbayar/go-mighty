// Package postgres provides PostgreSQL storage implementations for game and user data.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	_ "github.com/lib/pq" // Import the postgres driver for database/sql.
	"github.com/rs/zerolog/log"
)

// Store implements the storage interface for PostgreSQL.
type Store struct {
	db *sql.DB
}

// NewStoreWithDB creates a new Store instance using an existing sql.DB connection.
func NewStoreWithDB(db *sql.DB) *Store {
	return &Store{db: db}
}

// NewStore creates a new Store instance by opening a connection to PostgreSQL
// using the provided connection string.
func NewStore(connStr string) (*Store, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(context.Background()); err != nil {
		return nil, err
	}

	return &Store{db: db}, nil
}

// CreateGame inserts a new game record into the database.
func (s *Store) CreateGame(ctx context.Context, g *game.Game) (err error) {
	start := time.Now()
	defer func() {
		log.Debug().
			Str("component", "postgres").
			Str("op", "CreateGame").
			Str("game_id", g.ID).
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("CreateGame")
	}()

	query := `INSERT INTO games (id, status, version, created_at) VALUES ($1, $2, $3, $4)`
	_, err = s.db.ExecContext(ctx, query, g.ID, g.Status, g.Version, g.CreatedAt)

	return err
}

// SaveMove inserts a new move record into the database ledger.
// clientVersion represents the client's known game version at the time they submitted the move.
func (s *Store) SaveMove(ctx context.Context, moveType game.MoveType, playerID string, seat int, version, clientVersion int64, payload any, gameID string) (err error) {
	start := time.Now()
	defer func() {
		log.Debug().
			Str("component", "postgres").
			Str("op", "SaveMove").
			Str("game_id", gameID).
			Str("player_id", playerID).
			Str("move_type", string(moveType)).
			Int64("version", version).
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("SaveMove")
	}()

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// clientVersion represents the client's known game version at the time they submitted the move.
	query := `INSERT INTO moves (game_id, player_id, seat_no, version, client_version, move_type, payload, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`
	_, err = s.db.ExecContext(ctx, query, gameID, playerID, seat, version, clientVersion, string(moveType), payloadJSON)

	return err
}

// UpdateGameStatus updates the status and version of an existing game.
func (s *Store) UpdateGameStatus(ctx context.Context, gameID string, status game.Phase, version int64) (err error) {
	start := time.Now()
	defer func() {
		log.Debug().
			Str("component", "postgres").
			Str("op", "UpdateGameStatus").
			Str("game_id", gameID).
			Str("status", string(status)).
			Int64("version", version).
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("UpdateGameStatus")
	}()

	query := `UPDATE games SET status = $1, version = $2, updated_at = NOW() WHERE id = $3`
	_, err = s.db.ExecContext(ctx, query, status, version, gameID)

	return err
}

// ListGamesByStatus retrieves a list of game IDs with the specified status.
func (s *Store) ListGamesByStatus(ctx context.Context, status game.Phase) (ids []string, err error) {
	start := time.Now()
	defer func() {
		log.Debug().
			Str("component", "postgres").
			Str("op", "ListGamesByStatus").
			Str("status", string(status)).
			Int("count", len(ids)).
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("ListGamesByStatus")
	}()

	query := `SELECT id FROM games WHERE status = $1 ORDER BY created_at DESC LIMIT 50`

	rows, err := s.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}
