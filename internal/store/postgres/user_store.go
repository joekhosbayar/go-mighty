package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

// User represents a player's account information.
type User struct {
	ID         string
	Username   string
	CognitoSub string
	Email      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// UserStats represents a player's performance statistics.
type UserStats struct {
	UserID      string
	GamesPlayed int
	GamesWon    int
	TotalPoints float64
	UpdatedAt   time.Time
}

// GetUserByCognitoSub retrieves a user by their Cognito subject id.
func (s *Store) GetUserByCognitoSub(ctx context.Context, sub string) (*User, error) {
	query := `SELECT id, username, cognito_sub, email, created_at, updated_at FROM users WHERE cognito_sub = $1`

	var (
		user  User
		email sql.NullString
	)

	err := s.db.QueryRowContext(ctx, query, sub).Scan(&user.ID, &user.Username, &user.CognitoSub, &email, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}

		return nil, err
	}

	user.Email = email.String

	return &user, nil
}

// UpsertUserByCognitoSub returns the local user for a Cognito subject,
// creating one (with stats) on first sight. Safe under concurrent first
// requests: the insert is ON CONFLICT DO NOTHING keyed on cognito_sub.
// A username collision (preferred_username is not unique in Cognito)
// falls back to a sub-suffixed username.
func (s *Store) UpsertUserByCognitoSub(ctx context.Context, sub, username string) (*User, error) {
	if existing, err := s.GetUserByCognitoSub(ctx, sub); err != nil || existing != nil {
		return existing, err
	}

	insert := func(name string) error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO users (id, username, cognito_sub, email, created_at, updated_at)
			 VALUES ($1, $2, $3, NULL, NOW(), NOW())
			 ON CONFLICT (cognito_sub) DO NOTHING`, sub, name, sub); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_stats (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, sub); err != nil {
			return err
		}

		return tx.Commit()
	}

	err := insert(username)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			suffix := sub
			if len(suffix) > 8 {
				suffix = suffix[:8]
			}

			err = insert(username + "_" + suffix)
		}

		if err != nil {
			return nil, err
		}
	}

	return s.GetUserByCognitoSub(ctx, sub)
}

// GetUserStats retrieves the statistics for a specific user.
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
