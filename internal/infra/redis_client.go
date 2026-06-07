// Package infra provides infrastructure-level components like database and cache clients.
package infra

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// Redis is a wrapper around a Redis client.
type Redis struct {
	client ClientGetterSetter
}

// ClientGetterSetter is an interface that abstracts basic Redis operations.
type ClientGetterSetter interface {
	Ping(ctx context.Context) *redis.StatusCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
}

// Ping checks the connectivity to the Redis server.
func (c *Redis) Ping(ctx context.Context) (string, error) {
	pong, err := c.client.Ping(ctx).Result()
	return pong, err
}

// Set stores a string value in Redis with an optional expiration.
func (c *Redis) Set(ctx context.Context, key, val string, expiration time.Duration) error {
	err := c.client.Set(ctx, key, val, expiration).Err()
	return err
}

// Get retrieves a string value from Redis by its key.
func (c *Redis) Get(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, key).Result()
	return val, err
}

// NewRedis creates and returns a new Redis client instance.
func NewRedis() *Redis {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "redis:6379"
	}

	password := os.Getenv("REDIS_PASSWORD")

	db := 0

	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		if parsedDB, err := strconv.Atoi(dbStr); err != nil {
			log.Error().Err(err).Msgf("Invalid REDIS_DB value %q, defaulting to 0", dbStr)
		} else {
			db = parsedDB
		}
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	return &Redis{
		client: rdb,
	}
}
