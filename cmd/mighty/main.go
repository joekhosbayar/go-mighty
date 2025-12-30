package main

import (
	"context"
	"flag"
	"go-mighty/internal/api/router"
	"go-mighty/internal/infra"
	"net/http"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	setupLogger()
	redisClient := infra.ProvideRedisClient()
	pong, err := redisClient.PingRedis(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("Main method: redis ping failed")
	}
	log.Info().Str("Main method: pong", pong).Msg("redis ping success")

	postgresClient := infra.ProvidePostgresClient()
	err = postgresClient.Ping(context.Background())
	if err != nil {
		log.Fatal().Err(err).Msg("Main method: postgres ping failed")
	}

	setupRouter()
}

func setupRouter() {
	r := router.Route()
	err := http.ListenAndServe(":8080", r)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to start HTTP server!")
	}
}

func setupLogger() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	debug := flag.Bool("debug", false, "sets log level to debug")

	flag.Parse()

	// Default level for this example is info, unless debug flag is present
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	if *debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Debug().Msg("This message appears only when log level set to Debug")
	log.Info().Msg("This message appears when log level set to Debug or Info")

	if e := log.Debug(); e.Enabled() {
		// Compute log output only if enabled.
		value := "bar"
		e.Str("foo", value).Msg("some debug message")
	}
}
