// Package service provides the business logic for the Mighty application, including
// authentication and game management services.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	redisstore "github.com/joekhosbayar/go-mighty/internal/store/redis"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

var (
	// ErrRedisStoreNotInitialized is returned when the Redis store is not available.
	ErrRedisStoreNotInitialized = errors.New("redis store not initialized")
	// ErrGameNotFound is returned when a requested game does not exist.
	ErrGameNotFound = errors.New("game not found")
	// ErrGameFull is returned when a player attempts to join a game that already has 5 players.
	ErrGameFull = errors.New("game is full")
	// ErrGameBusy is returned when the game's lock cannot be acquired in time.
	ErrGameBusy = errors.New("game busy")
)

// RedisStore defines the interface for hot state storage of games in Redis.
type RedisStore interface {
	SaveGame(ctx context.Context, g *game.Game, expectedVersion int64) error
	LoadGame(ctx context.Context, gameID string) (*game.Game, error)
	AcquireLock(ctx context.Context, gameID string) (string, error)
	ReleaseLock(ctx context.Context, gameID, token string) error
	PublishEvent(ctx context.Context, gameID string, event any) error
	Subscribe(ctx context.Context, gameID string) *redis.PubSub
}

// Game service manages game lifecycle, including creation, joining, and move processing.
type Game struct {
	redisStore    RedisStore
	postgresStore *postgres.Store
}

// NewGame creates and returns a new Game service instance.
func NewGame(r RedisStore, p *postgres.Store) *Game {
	return &Game{
		redisStore:    r,
		postgresStore: p,
	}
}

// withGameLock acquires the game's distributed lock, mapping contention to ErrGameBusy.
func (s *Game) withGameLock(ctx context.Context, gameID string) (release func(), err error) {
	token, err := s.redisStore.AcquireLock(ctx, gameID)
	if err != nil {
		if errors.Is(err, redisstore.ErrLockFailed) {
			return nil, ErrGameBusy
		}

		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	releaseCtx := context.WithoutCancel(ctx)

	return func() { _ = s.redisStore.ReleaseLock(releaseCtx, gameID, token) }, nil
}

// CreateGame initializes a new game and persists it in both Postgres and Redis.
func (s *Game) CreateGame(ctx context.Context, id string, cfg game.GameConfig) (*game.Game, error) {
	g := game.NewWithConfig(id, cfg)

	// Save to Postgres (ledger)
	if err := s.postgresStore.CreateGame(ctx, g); err != nil {
		return nil, fmt.Errorf("failed to create game in db: %w", err)
	}

	// Save to Redis (hot state)
	if err := s.redisStore.SaveGame(ctx, g, 0); err != nil {
		return nil, fmt.Errorf("failed to save game in redis: %w", err)
	}

	return g, nil
}

// JoinGame adds a player to an existing game. If the player is already in the game,
// it refreshes their connection state. If not, it finds the first available seat.
// If the game becomes full after joining, it transitions the game to the bidding phase.
func (s *Game) JoinGame(ctx context.Context, gameID, playerID, playerName string) (*game.Game, error) {
	// Lock
	release, err := s.withGameLock(ctx, gameID)
	if err != nil {
		return nil, err
	}
	defer release()

	// Load
	g, err := s.redisStore.LoadGame(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to load game: %w", err)
	}

	if g == nil {
		return nil, ErrGameNotFound
	}

	loadedVersion := g.Version

	// Logic: Find seat
	seat := -1

	// First, check if player is already in the game
	for i, p := range g.Players {
		if p != nil && p.ID == playerID {
			g.Players[i].Name = playerName
			g.Players[i].IsConnected = true
			g.Version++
			g.UpdatedAt = time.Now()

			if err := s.redisStore.SaveGame(ctx, g, loadedVersion); err != nil {
				return nil, err
			}

			return g, nil // Already in seat; refresh connection state
		}
	}

	// If not already in the game, find the first available seat within the
	// configured number of seats.
	for i := 0; i < g.NumSeatsPublic(); i++ {
		if g.Players[i] == nil {
			seat = i
			break
		}
	}

	if seat == -1 {
		return nil, ErrGameFull
	}

	g.Players[seat] = &game.Player{ID: playerID, Name: playerName, Seat: seat, IsConnected: true, Hand: []game.Card{}, Points: []game.Card{}}
	g.Version++
	g.UpdatedAt = time.Now()

	// Check if game full -> Start
	if g.IsFull() {
		g.Start()
	}

	// Save
	if err := s.redisStore.SaveGame(ctx, g, loadedVersion); err != nil {
		return nil, err
	}

	// Save Move to Postgres (Join is a move?)
	// Architecture says "Inserts join move to Postgres ledger".
	if err := s.postgresStore.SaveMove(ctx, "join", playerID, seat, g.Version, g.Version-1, map[string]any{"name": playerName}, gameID); err != nil {
		return nil, fmt.Errorf("failed to save join move in db: %w", err)
	}

	// Publish
	_ = s.redisStore.PublishEvent(ctx, gameID, map[string]any{
		"type":    "player_joined",
		"player":  g.Players[seat],
		"version": g.Version,
	})

	return g, nil
}

// ProcessMove validates and applies a game move. It handles concurrency via a distributed lock
// and optimistic version checking. The move is persisted to the Postgres ledger and published
// to the game's event channel.
func (s *Game) ProcessMove(ctx context.Context, gameID, playerID string, moveType game.MoveType, payload any, clientVersion int64) (*game.Game, error) {
	// 1. Lock
	release, err := s.withGameLock(ctx, gameID)
	if err != nil {
		return nil, err
	}
	defer release()

	// 2. Load
	g, err := s.redisStore.LoadGame(ctx, gameID)
	if err != nil {
		return nil, err
	}

	if g == nil {
		return nil, ErrGameNotFound
	}

	loadedVersion := g.Version
	if clientVersion != loadedVersion {
		return nil, redisstore.ErrStaleVersion
	}

	// 3. Validate
	if err := g.ValidateMove(playerID, moveType, payload); err != nil {
		return nil, err
	}

	// 4. Apply
	if err := g.ApplyMove(playerID, moveType, payload); err != nil {
		return nil, err
	}

	// 5. Save Redis
	if err := s.redisStore.SaveGame(ctx, g, loadedVersion); err != nil {
		return nil, err
	}

	// 6. Save Postgres
	// Convert payload to appropriate type if needed?
	// Payload is interface{}, Postgres checks specific types or marshals JSON.
	// For moves like Discard (list of cards), we might need to be careful with JSON unmarshalling from HTTP request to domain types before calling this.
	// Assuming payload is already domain type.
	seat := -1

	p := g.GetPlayer(playerID)
	if p != nil {
		seat = p.Seat
	}

	if err := s.postgresStore.SaveMove(ctx, moveType, playerID, seat, g.Version, clientVersion, payload, gameID); err != nil {
		return nil, fmt.Errorf("failed to save move in db: %w", err)
	}

	// 7. Publish
	_ = s.redisStore.PublishEvent(ctx, gameID, map[string]any{
		"type":       "move",
		"move_type":  moveType,
		"player_id":  playerID,
		"payload":    payload,
		"version":    g.Version,
		"game_state": g, // send full state or delta? Full state is safer but heavier.
		// Architecture said: "Client must refresh state"?
		// Pub/Sub usually sends delta or "Something changed, fetch new state".
		// Or sends the event.
		// We can send the event.
	})

	return g, nil
}

// Subscribe returns a Redis PubSub channel for real-time game events.
func (s *Game) Subscribe(ctx context.Context, gameID string) *redis.PubSub {
	if s.redisStore == nil {
		return nil
	}

	return s.redisStore.Subscribe(ctx, gameID)
}

// GetGame retrieves the current state of a game from hot storage (Redis).
func (s *Game) GetGame(ctx context.Context, gameID string) (*game.Game, error) {
	if s.redisStore == nil {
		return nil, ErrRedisStoreNotInitialized
	}

	return s.redisStore.LoadGame(ctx, gameID)
}

// ListGamesByStatus retrieves a list of games with the specified status,
// combining data from Postgres and Redis to ensure accuracy.
func (s *Game) ListGamesByStatus(ctx context.Context, status game.Phase) ([]*game.Game, error) {
	if s.redisStore == nil {
		return nil, ErrRedisStoreNotInitialized
	}

	ids, err := s.postgresStore.ListGamesByStatus(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list games from db: %w", err)
	}

	var games []*game.Game

	for _, id := range ids {
		g, err := s.redisStore.LoadGame(ctx, id)
		if err != nil {
			log.Warn().Str("game_id", id).Err(err).Msg("failed to load game from redis")
			continue
		}

		if g != nil {
			// Only include games where status actually matches (Redis is truth for hot state)
			if g.Status == status {
				games = append(games, g)
			}
		}
	}

	return games, nil
}
