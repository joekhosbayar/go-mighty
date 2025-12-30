package infra

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

type RedisClient struct {
	client ClientGetterSetter
}

type ClientGetterSetter interface {
	Ping(ctx context.Context) *redis.StatusCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
}

func (c *RedisClient) PingRedis(ctx context.Context) (string, error) {
	pong, err := c.client.Ping(ctx).Result()
	return pong, err
}

func (c *RedisClient) SetVal(ctx context.Context, key string, val string, expiration time.Duration) error {
	err := c.client.Set(ctx, key, val, expiration).Err()
	return err
}

func (c *RedisClient) GetVal(ctx context.Context, key string) (string, error) {
	val, err := c.client.Get(ctx, key).Result()
	return val, err
}

func ProvideRedisClient() *RedisClient {
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

	return &RedisClient{
		client: rdb,
	}
}
