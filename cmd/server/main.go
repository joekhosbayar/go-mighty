package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/api"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"github.com/joekhosbayar/go-mighty/internal/store/redis"
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
	var pgStore *postgres.Store
	var err error
	for i := 0; i < 30; i++ {
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

	// 3. Service
	svc := service.NewGameService(redisStore, pgStore)

	// 4. API
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatalf("JWT_SECRET must be set")
	}
	authSvc := service.NewAuthService(pgStore, jwtSecret)
	handler := api.NewHandler(svc, authSvc)

	// 5. Router
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/signup", handler.SignupHandler)
	mux.HandleFunc("POST /auth/login", handler.LoginHandler)
	mux.HandleFunc("POST /games", handler.CreateGameHandler)
	mux.HandleFunc("POST /games/{id}/join", handler.JoinGameHandler)
	mux.HandleFunc("POST /games/{id}/move", handler.MoveHandler)
	mux.HandleFunc("GET /games/{id}", handler.GetGameHandler)
	mux.HandleFunc("GET /games/{id}/ws", handler.WSHandler) // WebSocket

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
