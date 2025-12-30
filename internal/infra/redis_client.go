package infra

import (
	"context"
	"fmt"
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

func (c *RedisClient) pingRedis(ctx context.Context) (string, error) {
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
	rdb := redis.NewClient(&redis.Options{
		Addr:     "redis:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	res, err := rdb.Ping(context.Background()).Result()
	if err != nil {
		log.Err(err).Msg("redis ping failed")
	} else {
		log.Info().Msg(fmt.Sprintf("redis ping success: %v", res))
	}
	return &RedisClient{
		client: rdb,
	}
}
