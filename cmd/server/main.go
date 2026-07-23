package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/api"
	"github.com/joekhosbayar/go-mighty/internal/infra"
	"github.com/joekhosbayar/go-mighty/internal/ratelimit"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	"github.com/joekhosbayar/go-mighty/internal/store/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// Safeguard tunables (spec Section 3). Named here, not inlined into the
// NewHandler call below, so the startup diagnostics log can echo the exact
// values that were actually applied instead of a second, driftable copy.
const (
	wsMessagesPerSec = 10
	wsMessageBurst   = 20
	connsPerUser     = 3
	connsPerIP       = 20
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

	// Comma-separated, e.g. "https://themighty.gg,https://www.themighty.gg".
	// Empty in local dev, where the same-host fallback applies.
	var allowedOrigins []string
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		allowedOrigins = strings.Split(raw, ",")
	}

	// Only true where an ingress proxy is the sole source of traffic — see
	// WithTrustedProxy. Set in the prod compose .env, absent locally.
	trustProxy := os.Getenv("TRUST_PROXY_HEADERS") == "true"

	handler := api.NewHandler(svc, authSvc,
		api.WithRateLimiter(limiter),
		api.WithAllowedOrigins(allowedOrigins),
		api.WithWSMessageRate(wsMessagesPerSec, wsMessageBurst),
		api.WithConnLimits(connsPerUser, connsPerIP),
		api.WithTrustedProxy(trustProxy))

	// Echo the resolved safeguard configuration once at startup. Two failure
	// modes are otherwise silent in production: a degenerate ALLOWED_ORIGINS
	// (e.g. "," or all-whitespace) collapses to zero entries and the origin
	// check falls back to the same-host dev behavior, which rejects every
	// browser WebSocket; and an unset or misspelled TRUST_PROXY_HEADERS makes
	// ClientIP return the proxy's own address for every request, turning the
	// per-IP cap into a global connection ceiling for the whole service.
	effectiveOrigins := handler.AllowedOrigins()

	originsDescription := "same-host fallback (ALLOWED_ORIGINS empty or unset)"
	if len(effectiveOrigins) > 0 {
		originsDescription = strings.Join(effectiveOrigins, ",")
	}

	if os.Getenv("ALLOWED_ORIGINS") != "" && len(effectiveOrigins) == 0 {
		zlog.Warn().
			Str("allowed_origins_raw", os.Getenv("ALLOWED_ORIGINS")).
			Msg("ALLOWED_ORIGINS was set but normalized to zero entries; falling back to same-host origin check, which rejects every browser WebSocket in production — this is almost certainly a deploy mistake")
	}

	zlog.Info().
		Str("allowedOrigins", originsDescription).
		Bool("trustProxy", trustProxy).
		Int("connLimitPerUser", connsPerUser).
		Int("connLimitPerIP", connsPerIP).
		Float64("wsMessagesPerSec", wsMessagesPerSec).
		Float64("wsMessageBurst", wsMessageBurst).
		Msg("resolved safeguard configuration")

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

	// ReadTimeout and WriteTimeout are deliberately unset: both apply to
	// hijacked connections and would kill long-lived WebSockets mid-game.
	// ReadHeaderTimeout is the safe one — it bounds slowloris-style header
	// stalls without touching an established socket.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler.LoggingMiddleware(api.BodyLimitMiddleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
