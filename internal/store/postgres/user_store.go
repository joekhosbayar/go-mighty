package postgres

import (
	"context"
	"database/sql"
	"time"
)

type User struct {
	ID           string
	Username     string
	PasswordHash string
	Email        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserStats struct {
	UserID      string
	GamesPlayed int
	GamesWon    int
	TotalPoints float64
	UpdatedAt   time.Time
}

func (s *Store) CreateUser(ctx context.Context, user *User) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `INSERT INTO users (id, username, password_hash, email, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err = tx.ExecContext(ctx, query, user.ID, user.Username, user.PasswordHash, user.Email, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		return err
	}

	// Initialize stats
	queryStats := `INSERT INTO user_stats (user_id) VALUES ($1)`
	_, err = tx.ExecContext(ctx, queryStats, user.ID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	query := `SELECT id, username, password_hash, email, created_at, updated_at FROM users WHERE username = $1`
	var user User
	err := s.db.QueryRowContext(ctx, query, username).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Email, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *Store) GetUserStats(ctx context.Context, userID string) (*UserStats, error) {
	query := `SELECT user_id, games_played, games_won, total_points, updated_at FROM user_stats WHERE user_id = $1`
	var stats UserStats
	err := s.db.QueryRowContext(ctx, query, userID).Scan(&stats.UserID, &stats.GamesPlayed, &stats.GamesWon, &stats.TotalPoints, &stats.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &stats, nil
}
