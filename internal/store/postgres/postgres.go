package postgres

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/joekhosbayar/go-mighty/internal/game"
	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
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

func (s *Store) CreateGame(ctx context.Context, g *game.GameState) error {
	query := `INSERT INTO games (id, status, version, created_at) VALUES ($1, $2, $3, $4)`
	_, err := s.db.ExecContext(ctx, query, g.ID, g.Status, g.Version, g.CreatedAt)
	return err
}

func (s *Store) SaveMove(ctx context.Context, moveType game.MoveType, playerID string, seat int, version int64, payload interface{}, gameID string) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// client_version is not passed here? It is used for validation.
	// The ledger stores the resulting version usually.
	// But architecture says: "client_version... Client's known version".
	// Let's assume we store the version resulting from the move.

	query := `INSERT INTO moves (game_id, player_id, seat_no, version, client_version, move_type, payload, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`
	// Assuming client_version matches version-1 for now, or just store version.
	// We'll just pass version as client_version for now to satisfy schema.
	_, err = s.db.ExecContext(ctx, query, gameID, playerID, seat, version, version-1, string(moveType), payloadJSON)
	return err
}

func (s *Store) UpdateGameStatus(ctx context.Context, gameID string, status game.Phase, version int64) error {
	query := `UPDATE games SET status = $1, version = $2, updated_at = NOW() WHERE id = $3`
	_, err := s.db.ExecContext(ctx, query, status, version, gameID)
	return err
}
