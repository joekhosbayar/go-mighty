package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

type Store struct {
	db *sql.DB
}

func NewStoreWithDB(db *sql.DB) *Store {
	return &Store{db: db}
}

func NewStore(connStr string) (*Store, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) CreateGame(ctx context.Context, g *game.GameState) (err error) {
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

func (s *Store) SaveMove(ctx context.Context, moveType game.MoveType, playerID string, seat int, version int64, clientVersion int64, payload interface{}, gameID string) (err error) {
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
	defer rows.Close()

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
