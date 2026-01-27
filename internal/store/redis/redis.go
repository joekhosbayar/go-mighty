package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
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

func (s *Store) SaveGame(ctx context.Context, g *game.GameState) (err error) {
	start := time.Now()
	key := s.Key(g.ID)
	defer func() {
		event := log.Debug().
			Str("component", "redis").
			Str("op", "SaveGame").
			Str("key", key).
			Dur("latency", time.Since(start))
		if err != nil {
			event.Err(err).Msg("SaveGame failed")
		} else {
			event.
				Str("game_id", g.ID).
				Int64("version", g.Version).
				Str("status", string(g.Status)).
				Msg("SaveGame success")
		}
	}()

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

func (s *Store) LoadGame(ctx context.Context, gameID string) (g *game.GameState, err error) {
	start := time.Now()
	key := s.Key(gameID)
	defer func() {
		event := log.Debug().
			Str("component", "redis").
			Str("op", "LoadGame").
			Str("key", key).
			Dur("latency", time.Since(start))
		if err != nil {
			event.Err(err).Msg("LoadGame failed")
		} else if g == nil {
			event.Msg("LoadGame not found")
		} else {
			event.
				Str("game_id", g.ID).
				Int64("version", g.Version).
				Str("status", string(g.Status)).
				Msg("LoadGame success")
		}
	}()

	data, err := s.client.Get(ctx, key+":state").Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Not found
		}
		return nil, err
	}

	var loadedGame game.GameState
	if err := json.Unmarshal(data, &loadedGame); err != nil {
		return nil, err
	}
	return &loadedGame, nil
}

// AcquireLock acquires a distributed lock for the game
func (s *Store) AcquireLock(ctx context.Context, gameID string) (locked bool, err error) {
	start := time.Now()
	key := s.Key(gameID) + ":lock"
	defer func() {
		log.Debug().
			Str("component", "redis").
			Str("op", "AcquireLock").
			Str("key", key).
			Bool("locked", locked).
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("AcquireLock")
	}()
	// Simple setnx with expiration
	return s.client.SetNX(ctx, key, "locked", 5*time.Second).Result()
}

func (s *Store) ReleaseLock(ctx context.Context, gameID string) (err error) {
	start := time.Now()
	key := s.Key(gameID) + ":lock"
	defer func() {
		log.Debug().
			Str("component", "redis").
			Str("op", "ReleaseLock").
			Str("key", key).
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("ReleaseLock")
	}()
	return s.client.Del(ctx, key).Err()
}

// CheckVersion checks if client version matches server version
func (s *Store) CheckVersion(ctx context.Context, gameID string, clientVersion int64) (err error) {
	start := time.Now()
	key := s.Key(gameID) + ":version"
	defer func() {
		log.Debug().
			Str("component", "redis").
			Str("op", "CheckVersion").
			Str("key", key).
			Int64("client_version", clientVersion).
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("CheckVersion")
	}()

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

func (s *Store) PublishEvent(ctx context.Context, gameID string, event interface{}) (err error) {
	start := time.Now()
	channel := s.Key(gameID) + ":events"
	defer func() {
		logEvent := log.Debug().
			Str("component", "redis").
			Str("op", "PublishEvent").
			Str("channel", channel)
		
		// Try to extract event type from map if it exists
		if eventMap, ok := event.(map[string]interface{}); ok {
			if eventType, hasType := eventMap["type"]; hasType {
				logEvent = logEvent.Interface("event_type", eventType)
			} else {
				logEvent = logEvent.Str("event_type", fmt.Sprintf("%T", event))
			}
		} else {
			logEvent = logEvent.Str("event_type", fmt.Sprintf("%T", event))
		}
		
		logEvent.Err(err).
			Dur("latency", time.Since(start)).
			Msg("PublishEvent")
	}()

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.client.Publish(ctx, channel, data).Err()
}

func (s *Store) Subscribe(ctx context.Context, gameID string) *redis.PubSub {
	channel := s.Key(gameID) + ":events"
	log.Debug().
		Str("component", "redis").
		Str("op", "Subscribe").
		Str("channel", channel).
		Msg("Subscribing to channel")
	return s.client.Subscribe(ctx, channel)
}
