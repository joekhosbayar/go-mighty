package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/redis/go-redis/v9"
)

var (
	ErrLockFailed   = errors.New("failed to acquire lock")
	ErrStaleVersion = errors.New("stale version")
)

type Store struct {
	client *redis.Client
}

func NewStore(addr string) *Store {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &Store{client: client}
}

func (s *Store) Key(gameID string) string {
	return fmt.Sprintf("game:%s", gameID)
}

func (s *Store) SaveGame(ctx context.Context, g *game.GameState) error {
	key := s.Key(g.ID)
	data, err := json.Marshal(g)
	if err != nil {
		return err
	}
	// Use pipeline?
	// Save state
	err = s.client.Set(ctx, key+":state", data, 24*time.Hour).Err()
	if err != nil {
		return err
	}
	// Save version separate? No, version is in GameState.
	// But architecture said version key separate?
	// "Key: game:{gameID}:version... for optimistic locking"
	// If version is in state, we don't strictly need separate key unless for quick check.
	// I will save separate version key as per architecture.
	return s.client.Set(ctx, key+":version", g.Version, 24*time.Hour).Err()
}

func (s *Store) LoadGame(ctx context.Context, gameID string) (*game.GameState, error) {
	key := s.Key(gameID)
	data, err := s.client.Get(ctx, key+":state").Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Not found
		}
		return nil, err
	}

	var g game.GameState
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// AcquireLock acquires a distributed lock for the game
func (s *Store) AcquireLock(ctx context.Context, gameID string) (bool, error) {
	key := s.Key(gameID) + ":lock"
	// Simple setnx with expiration
	return s.client.SetNX(ctx, key, "locked", 5*time.Second).Result()
}

func (s *Store) ReleaseLock(ctx context.Context, gameID string) error {
	key := s.Key(gameID) + ":lock"
	return s.client.Del(ctx, key).Err()
}

// CheckVersion checks if client version matches server version
func (s *Store) CheckVersion(ctx context.Context, gameID string, clientVersion int64) error {
	key := s.Key(gameID) + ":version"
	val, err := s.client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			// If no version found, assumes 0
			if clientVersion == 0 {
				return nil
			}
			return ErrStaleVersion
		}
		return err
	}
	if val != clientVersion {
		return ErrStaleVersion
	}
	return nil
}

func (s *Store) PublishEvent(ctx context.Context, gameID string, event interface{}) error {
	channel := s.Key(gameID) + ":events"
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.client.Publish(ctx, channel, data).Err()
}

func (s *Store) Subscribe(ctx context.Context, gameID string) *redis.PubSub {
	channel := s.Key(gameID) + ":events"
	return s.client.Subscribe(ctx, channel)
}
