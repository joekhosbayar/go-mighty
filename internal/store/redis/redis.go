package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

var (
	// ErrLockFailed is returned when a distributed lock cannot be acquired.
	ErrLockFailed = errors.New("failed to acquire lock")
	// ErrStaleVersion is returned when a version check fails.
	ErrStaleVersion = errors.New("stale version")
)

// Store implements hot state storage for games in Redis.
type Store struct {
	client *redis.Client
}

// NewStore creates a new Store instance using the provided Redis address.
func NewStore(addr string) *Store {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	return &Store{client: client}
}

// Key returns the Redis key for a specific game ID.
func (s *Store) Key(gameID string) string {
	return "game:" + gameID
}

// SaveGame serializes and persists the game state in Redis.
func (s *Store) SaveGame(ctx context.Context, g *game.Game) (err error) {
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

// LoadGame retrieves and deserializes the game state from Redis.
func (s *Store) LoadGame(ctx context.Context, gameID string) (g *game.Game, err error) {
	start := time.Now()

	key := s.Key(gameID)
	defer func() {
		event := log.Debug().
			Str("component", "redis").
			Str("op", "LoadGame").
			Str("key", key).
			Dur("latency", time.Since(start))
		switch {
		case err != nil:
			event.Err(err).Msg("LoadGame failed")
		case g == nil:
			event.Msg("LoadGame not found")
		default:
			event.
				Str("game_id", g.ID).
				Int64("version", g.Version).
				Str("status", string(g.Status)).
				Msg("LoadGame success")
		}
	}()

	data, err := s.client.Get(ctx, key+":state").Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // Not found
		}

		return nil, err
	}

	var loadedGame game.Game
	if err := json.Unmarshal(data, &loadedGame); err != nil {
		return nil, err
	}

	return &loadedGame, nil
}

// lockBackoff is the retry schedule when the lock is contended.
var lockBackoff = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}

// AcquireLock acquires a distributed lock for the game with a 5-second expiration.
// It returns an ownership token required to release the lock, retrying with
// backoff while contended. Returns ErrLockFailed if the lock stays held.
func (s *Store) AcquireLock(ctx context.Context, gameID string) (token string, err error) {
	start := time.Now()

	key := s.Key(gameID) + ":lock"
	defer func() {
		log.Debug().
			Str("component", "redis").
			Str("op", "AcquireLock").
			Str("key", key).
			Bool("locked", token != "").
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("AcquireLock")
	}()

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	candidate := hex.EncodeToString(raw)

	for attempt := 0; ; attempt++ {
		ok, err := s.client.SetNX(ctx, key, candidate, 5*time.Second).Result()
		if err != nil {
			return "", err
		}

		if ok {
			return candidate, nil
		}

		if attempt >= len(lockBackoff) {
			return "", ErrLockFailed
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(lockBackoff[attempt]):
		}
	}
}

// releaseScript deletes the lock only when the caller still owns it.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0`)

// ReleaseLock releases the distributed lock if token matches the current owner.
func (s *Store) ReleaseLock(ctx context.Context, gameID, token string) (err error) {
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

	return releaseScript.Run(ctx, s.client, []string{key}, token).Err()
}

// CheckVersion checks if client version matches server version.
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
		if errors.Is(err, redis.Nil) {
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

// PublishEvent marshals and publishes an event to the game's Redis PubSub channel.
func (s *Store) PublishEvent(ctx context.Context, gameID string, event any) (err error) {
	start := time.Now()

	channel := s.Key(gameID) + ":events"
	defer func() {
		logEvent := log.Debug().
			Str("component", "redis").
			Str("op", "PublishEvent").
			Str("channel", channel)

		// Try to extract event type from map if it exists
		if eventMap, ok := event.(map[string]any); ok {
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

// Subscribe returns a Redis PubSub channel for game events.
func (s *Store) Subscribe(ctx context.Context, gameID string) *redis.PubSub {
	channel := s.Key(gameID) + ":events"
	log.Debug().
		Str("component", "redis").
		Str("op", "Subscribe").
		Str("channel", channel).
		Msg("Subscribing to channel")

	return s.client.Subscribe(ctx, channel)
}
