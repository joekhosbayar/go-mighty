package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/api"
	"github.com/joekhosbayar/go-mighty/internal/infra"
	"github.com/joekhosbayar/go-mighty/internal/ratelimit"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"github.com/joekhosbayar/go-mighty/internal/store/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func main() {
	// 0. Logging Config
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "debug" // Default to debug as requested
	}

	level, parseErr := zerolog.ParseLevel(logLevel)
	if parseErr != nil {
		log.Printf("Invalid LOG_LEVEL %q: %v; falling back to %s", logLevel, parseErr, zerolog.DebugLevel.String())
		level = zerolog.DebugLevel
	}

	zerolog.SetGlobalLevel(level)
	// 1. Config
	pgConn := os.Getenv("POSTGRES_CONN")
	if pgConn == "" {
		pgConn = "postgres://postgres:mightypassword@localhost:5432/postgres?sslmode=disable"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	// 2. Store
	var (
		pgStore *postgres.Store
		err     error
	)
	for i := range 30 {
		pgStore, err = postgres.NewStore(pgConn)
		if err == nil {
			break
		}

		log.Printf("Failed to connect to Postgres (attempt %d/30): %v", i+1, err)
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		log.Fatalf("Failed to connect to Postgres after 30 attempts: %v", err)
	}

	redisStore := redis.NewStore(redisAddr)

	// A separate client for the limiter: the game store's client is private
	// to that package, and one extra small pool is cheaper than widening its
	// API surface.
	rlClient := goredis.NewClient(&goredis.Options{Addr: redisAddr})
	defer func() { _ = rlClient.Close() }()

	limiter := ratelimit.New(rlClient)

	// 3. Service
	svc := service.NewGame(redisStore, pgStore)

	// 4. API
	cognitoPoolID := os.Getenv("COGNITO_POOL_ID")
	cognitoClientID := os.Getenv("COGNITO_CLIENT_ID")

	if cognitoPoolID == "" || cognitoClientID == "" {
		log.Fatalf("COGNITO_POOL_ID and COGNITO_CLIENT_ID must be set")
	}

	cognitoRegion := os.Getenv("COGNITO_REGION")
	if cognitoRegion == "" {
		cognitoRegion = "us-east-1"
	}

	ctx := context.Background()
	issuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", cognitoRegion, cognitoPoolID)

	fetcher, err := infra.NewCognitoAttributesFetcher(ctx, cognitoRegion, cognitoPoolID)
	if err != nil {
		log.Fatalf("cognito attributes fetcher: %v", err)
	}

	authSvc, err := service.NewCognitoAuth(ctx, pgStore, fetcher, issuer, cognitoClientID)
	if err != nil {
		log.Fatalf("cognito auth: %v", err)
	}

	handler := api.NewHandler(svc, authSvc, api.WithRateLimiter(limiter))

	// 5. Router
	mux := http.NewServeMux()
	mux.HandleFunc("GET /games", handler.ListGamesHandler)
	mux.Handle("POST /games", handler.RequireAuth(
		handler.RateLimitByUser("creategame", ratelimit.PerHour(10))(
			http.HandlerFunc(handler.CreateGameHandler))))
	mux.HandleFunc("POST /games/{id}/join", handler.JoinGameHandler)
	mux.HandleFunc("POST /games/{id}/move", handler.MoveHandler)
	mux.HandleFunc("GET /games/{id}", handler.GetGameHandler)
	mux.HandleFunc("GET /games/{id}/ws", handler.WSHandler) // WebSocket
	mux.HandleFunc("GET /healthz", api.HealthzHandler)

	// 6. Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)

	if err := http.ListenAndServe(":"+port, api.LoggingMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
