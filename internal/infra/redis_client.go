package infra

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

func ProvideRedisClient() *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "redis:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	pong, err := rdb.Ping(context.Background()).Result()
	log.Info().Msg(fmt.Sprintf("Redis isConnected at init time: %s", pong))
	if err != nil {
		log.Error().Msg(fmt.Sprintf("Failed to connect to redis at init time: %s", err.Error()))
	}
	return rdb
}
