package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

var ErrRedisStoreNotInitialized = errors.New("redis store not initialized")

type RedisStore interface {
	SaveGame(ctx context.Context, g *game.GameState) error
	LoadGame(ctx context.Context, gameID string) (*game.GameState, error)
	AcquireLock(ctx context.Context, gameID string) (bool, error)
	ReleaseLock(ctx context.Context, gameID string) error
	CheckVersion(ctx context.Context, gameID string, clientVersion int64) error
	PublishEvent(ctx context.Context, gameID string, event interface{}) error
	Subscribe(ctx context.Context, gameID string) *redis.PubSub
}

type GameService struct {
	redisStore    RedisStore
	postgresStore *postgres.Store
}

func NewGameService(r RedisStore, p *postgres.Store) *GameService {
	return &GameService{
		redisStore:    r,
		postgresStore: p,
	}
}

// CreateGame initializes a new game
func (s *GameService) CreateGame(ctx context.Context, id string) (*game.GameState, error) {
	g := game.NewGame(id)

	// Save to Postgres (ledger)
	if err := s.postgresStore.CreateGame(ctx, g); err != nil {
		return nil, fmt.Errorf("failed to create game in db: %w", err)
	}

	// Save to Redis (hot state)
	if err := s.redisStore.SaveGame(ctx, g); err != nil {
		return nil, fmt.Errorf("failed to save game in redis: %w", err)
	}

	return g, nil
}

// JoinGame handles player joining
func (s *GameService) JoinGame(ctx context.Context, gameID, playerID, playerName string) (*game.GameState, error) {
	// Lock
	_, err := s.redisStore.AcquireLock(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer s.redisStore.ReleaseLock(ctx, gameID)

	// Load
	g, err := s.redisStore.LoadGame(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to load game: %w", err)
	}
	if g == nil {
		return nil, fmt.Errorf("game not found")
	}

	// Logic: Find seat
	seat := -1
	
	// First, check if player is already in the game
	for i, p := range g.Players {
		if p != nil && p.ID == playerID {
			g.Players[i].Name = playerName
			g.Players[i].IsConnected = true
			g.Version++
			g.UpdatedAt = time.Now()

			if err := s.redisStore.SaveGame(ctx, g); err != nil {
				return nil, err
			}
			return g, nil // Already in seat; refresh connection state
		}
	}

	// If not already in the game, find the first available seat
	for i, p := range g.Players {
		if p == nil {
			seat = i
			break
		}
	}

	if seat == -1 {
		return nil, fmt.Errorf("game is full")
	}

	g.Players[seat] = &game.Player{ID: playerID, Name: playerName, Seat: seat, IsConnected: true, Hand: []game.Card{}, Points: []game.Card{}}
	g.Version++
	g.UpdatedAt = time.Now()

	// Check if game full -> Start
	if g.IsFull() {
		g.Start()
	}

	// Save
	if err := s.redisStore.SaveGame(ctx, g); err != nil {
		return nil, err
	}

	// Save Move to Postgres (Join is a move?)
	// Architecture says "Inserts join move to Postgres ledger".
	if err := s.postgresStore.SaveMove(ctx, "join", playerID, seat, g.Version, g.Version-1, map[string]interface{}{"name": playerName}, gameID); err != nil {
		return nil, fmt.Errorf("failed to save join move in db: %w", err)
	}

	// Publish
	s.redisStore.PublishEvent(ctx, gameID, map[string]interface{}{
		"type":    "player_joined",
		"player":  g.Players[seat],
		"version": g.Version,
	})

	return g, nil
}

// ProcessMove handles game moves
func (s *GameService) ProcessMove(ctx context.Context, gameID, playerID string, moveType game.MoveType, payload interface{}, clientVersion int64) (*game.GameState, error) {
	// 1. Lock
	_, err := s.redisStore.AcquireLock(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer s.redisStore.ReleaseLock(ctx, gameID)

	// 2. Check Version
	// Reuse err variable for subsequent operations
	err = s.redisStore.CheckVersion(ctx, gameID, clientVersion)
	if err != nil {
		return nil, err // Returns stale version error
	}

	// 3. Load
	g, err := s.redisStore.LoadGame(ctx, gameID)
	if err != nil {
		return nil, err
	}

	// 4. Validate
	if err := g.ValidateMove(playerID, moveType, payload); err != nil {
		return nil, err
	}

	// 5. Apply
	if err := g.ApplyMove(playerID, moveType, payload); err != nil {
		return nil, err
	}

	// 6. Save Redis
	if err := s.redisStore.SaveGame(ctx, g); err != nil {
		return nil, err
	}

	// 7. Save Postgres
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

	// 8. Publish
	s.redisStore.PublishEvent(ctx, gameID, map[string]interface{}{
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

// Subscribe returns a redis PubSub
func (s *GameService) Subscribe(ctx context.Context, gameID string) *redis.PubSub {
	if s.redisStore == nil {
		return nil
	}
	return s.redisStore.Subscribe(ctx, gameID)
}

// GetGame retrieves game state
func (s *GameService) GetGame(ctx context.Context, gameID string) (*game.GameState, error) {
	if s.redisStore == nil {
		return nil, ErrRedisStoreNotInitialized
	}
	return s.redisStore.LoadGame(ctx, gameID)
}

// ListGamesByStatus retrieves a list of games with the specified status
func (s *GameService) ListGamesByStatus(ctx context.Context, status game.Phase) ([]*game.GameState, error) {
	if s.redisStore == nil {
		return nil, ErrRedisStoreNotInitialized
	}

	ids, err := s.postgresStore.ListGamesByStatus(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("failed to list games from db: %w", err)
	}

	var games []*game.GameState
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
