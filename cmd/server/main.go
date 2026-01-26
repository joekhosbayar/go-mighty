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
)

func main() {
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
	handler := api.NewHandler(svc)

	// 5. Router
	mux := http.NewServeMux()
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
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
