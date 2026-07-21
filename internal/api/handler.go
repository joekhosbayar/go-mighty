package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/ratelimit"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// GameService defines the interface for game logic operations.
type GameService interface {
	CreateGame(ctx context.Context, id string, cfg game.GameConfig) (*game.Game, error)
	JoinGame(ctx context.Context, gameID, playerID, playerName string) (*game.Game, error)
	ProcessMove(ctx context.Context, gameID, playerID string, moveType game.MoveType, payload any, clientVersion int64) (*game.Game, error)
	Subscribe(ctx context.Context, gameID string) *redis.PubSub
	GetGame(ctx context.Context, gameID string) (*game.Game, error)
	ListGamesByStatus(ctx context.Context, status game.Phase) ([]*game.Game, error)
}

// TokenValidator authenticates bearer tokens into local user claims.
type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (*service.AuthClaims, error)
}

// Handler handles HTTP requests for the game API.
type Handler struct {
	svc              GameService
	authSvc          TokenValidator
	limiter          *ratelimit.Limiter
	allowedOrigins   []string
	wsMessagesPerSec float64
	wsMessageBurst   float64
	conns            *connRegistry
	trustProxy       bool
}

// NewHandler creates a new Handler with the given services. Options carry the
// production safeguards (rate limiting, origin allowlist, connection caps);
// with none supplied the handler behaves exactly as it did before they were
// added, which keeps local dev and the existing tests simple.
func NewHandler(svc GameService, authSvc TokenValidator, opts ...Option) *Handler {
	h := &Handler{
		svc:     svc,
		authSvc: authSvc,
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

func (h *Handler) authenticate(r *http.Request) (*service.AuthClaims, error) {
	// RequireAuth already validated this request; don't pay for a second
	// JWKS verification just because the handler still calls authenticate.
	if claims, ok := ClaimsFromContext(r.Context()); ok {
		return claims, nil
	}

	if h.authSvc == nil {
		return nil, errors.New("authentication service is not configured")
	}

	tokenString := ""

	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Fields(authHeader)
		if len(parts) == 2 && parts[0] == "Bearer" {
			tokenString = parts[1]
		}
	}

	if tokenString == "" {
		return nil, fmt.Errorf("%w: missing authentication token", service.ErrInvalidToken)
	}

	return h.authSvc.ValidateToken(r.Context(), tokenString)
}

// writeAuthError maps an authenticate() error to the appropriate HTTP
// response: token problems (including a missing/malformed Authorization
// header) stay 401, while any other failure (JWKS fetch, store upsert, etc.)
// is treated as an infrastructure failure and reported as 503 so clients
// don't mistake "auth backend is down" for "your token is invalid".
func writeAuthError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrInvalidToken) {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
}

// CreateGameHandler - POST /games.
func (h *Handler) CreateGameHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}

	actualID := uuid.NewString()

	cfg := game.DefaultConfig()
	if r.Body != nil {
		var req struct {
			NumPlayers        int    `json:"num_players"`
			AllowJokerPartner *bool  `json:"allow_joker_partner"`
			FailDist          string `json:"fail_dist"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if req.NumPlayers == 4 || req.NumPlayers == 5 {
				cfg.NumPlayers = req.NumPlayers
			}
			if req.AllowJokerPartner != nil {
				cfg.AllowJokerPartner = *req.AllowJokerPartner
			}
			switch game.FailDist(req.FailDist) {
			case game.FailEqualSplit, game.FailDeclarerAlone, game.FailTwoOneSplit:
				cfg.FailDist = game.FailDist(req.FailDist)
			}
		}
	}

	// Create the game
	g, err := h.svc.CreateGame(r.Context(), actualID, cfg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-join the creator at seat 0
	updatedState, err := h.svc.JoinGame(r.Context(), actualID, claims.UserID, claims.Username)
	if err != nil {
		log.Error().Str("game_id", g.ID).Str("user_id", claims.UserID).Err(err).Msg("Failed to auto-join creator")
		http.Error(w, "failed to auto-join creator", http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updatedState)
}

// JoinGameHandler - POST /games/{id}/join.
func (h *Handler) JoinGameHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}

	gameID := r.PathValue("id")

	g, err := h.svc.JoinGame(r.Context(), gameID, claims.UserID, claims.Username)
	if err != nil {
		if errors.Is(err, service.ErrGameNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		if errors.Is(err, service.ErrGameFull) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		if errors.Is(err, service.ErrGameBusy) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(g)
}

// MoveHandler - POST /games/{id}/move.
func (h *Handler) MoveHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r)
	if err != nil {
		writeAuthError(w, err)
		return
	}

	gameID := r.PathValue("id")

	type Request struct {
		PlayerID      string        `json:"player_id"`
		MoveType      game.MoveType `json:"move_type"`
		Payload       any           `json:"payload"`
		ClientVersion int64         `json:"client_version"`
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	req.PlayerID = claims.UserID

	convertedPayload, err := ConvertPayload(req.MoveType, req.Payload)
	if err != nil {
		http.Error(w, "invalid payload structure: "+err.Error(), http.StatusBadRequest)
		return
	}

	g, err := h.svc.ProcessMove(r.Context(), gameID, req.PlayerID, req.MoveType, convertedPayload, req.ClientVersion)
	if err != nil {
		if errors.Is(err, service.ErrGameBusy) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		http.Error(w, err.Error(), http.StatusBadRequest) // Assume generic 400 for logic error
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(g)
}

// ConvertPayload converts generic map/interface to concrete struct.
func ConvertPayload(moveType game.MoveType, payload any) (any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	switch moveType {
	case game.MoveBid:
		var lastBid game.Bid
		if err := json.Unmarshal(data, &lastBid); err != nil {
			return nil, err
		}

		return lastBid, nil
	case game.MoveDiscard:
		var cards []game.Card
		if err := json.Unmarshal(data, &cards); err != nil {
			return nil, err
		}

		return cards, nil
	case game.MoveCallPartner:
		var move game.CallPartnerMove
		if err := json.Unmarshal(data, &move); err != nil {
			return nil, err
		}

		if move.Card == nil && !move.NoFriend {
			// Legacy shape: the payload is the card itself.
			var card game.Card
			if err := json.Unmarshal(data, &card); err == nil && card.Rank != "" {
				return game.CallPartnerMove{Card: &card}, nil
			}

			return nil, errors.New("call_partner requires a card or no_friend")
		}

		return move, nil
	case game.MovePlayCard:
		// Attempt to unmarshal as PlayCardMove first
		var playMove game.PlayCardMove
		if err := json.Unmarshal(data, &playMove); err == nil && playMove.Card.Rank != "" {
			return playMove, nil
		}
		// Fallback for raw Card payload
		var card game.Card
		if err := json.Unmarshal(data, &card); err == nil && card.Rank != "" {
			return game.PlayCardMove{Card: card}, nil
		}

		return nil, errors.New("invalid play card payload: expected card or play_card_move object")
	case game.MoveChangeConfig:
		var cm game.ChangeConfigMove
		if err := json.Unmarshal(data, &cm); err != nil {
			return nil, err
		}
		return cm, nil
	case game.MovePass, game.MovePlayAgain:
		return nil, nil // No payload needed for pass or play_again
	default:
		return payload, nil
	}
}

// GetGameHandler - GET /games/{id}.
func (h *Handler) GetGameHandler(w http.ResponseWriter, r *http.Request) {
	gameID := r.PathValue("id")

	g, err := h.svc.GetGame(r.Context(), gameID)
	if err != nil {
		if errors.Is(err, service.ErrRedisStoreNotInitialized) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		http.Error(w, err.Error(), http.StatusNotFound)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(g)
}

// ListGamesHandler - GET /games.
func (h *Handler) ListGamesHandler(w http.ResponseWriter, r *http.Request) {
	// Query param 'status' (e.g. ?status=waiting)
	statusParam := r.URL.Query().Get("status")
	if statusParam == "" {
		statusParam = string(game.PhaseWaiting)
	}

	status := game.Phase(statusParam)
	switch status {
	case game.PhaseWaiting, game.PhaseBidding, game.PhaseExchanging, game.PhaseCalling, game.PhasePlaying, game.PhaseFinished:
	default:
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	games, err := h.svc.ListGamesByStatus(r.Context(), status)
	if err != nil {
		if errors.Is(err, service.ErrRedisStoreNotInitialized) {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	// Make sure we return an empty array instead of null if no games are found
	if games == nil {
		games = []*game.Game{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(games)
}

// LoggingMiddleware logs the incoming HTTP requests and their responses.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Info().
			Str("method", req.Method).
			Str("url", req.URL.String()).
			Str("remote", req.RemoteAddr).
			Msg("Incoming request")

		lrw := &LoggingResponseWriter{ResponseWriter: w}
		start := time.Now()

		next.ServeHTTP(lrw, req)

		// If responseCode is 0, neither WriteHeader nor Write was called
		// This should be treated as 200 OK per HTTP specification
		statusCode := lrw.responseCode
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		event := log.Info()
		msg := "Success response"

		switch {
		case statusCode >= 500:
			event = log.Error()
			msg = "5xx response"
		case statusCode >= 400:
			event = log.Warn()
			msg = "4xx response"
		case statusCode >= 300:
			msg = "3xx redirection response"
		case statusCode >= 100 && statusCode < 200:
			msg = "1xx informational response"
		}

		event.
			Str("method", req.Method).
			Str("url", req.URL.String()).
			Str("remote", req.RemoteAddr).
			Int("responseCode", statusCode).
			Dur("duration", time.Since(start)).
			Msg(msg)
	})
}

// LoggingResponseWriter is a wrapper around http.ResponseWriter that captures the status code.
type LoggingResponseWriter struct {
	http.ResponseWriter
	responseCode int
	wroteHeader  bool
}

// WriteHeader captures the status code and calls the underlying ResponseWriter's WriteHeader.
func (lrw *LoggingResponseWriter) WriteHeader(code int) {
	if !lrw.wroteHeader {
		lrw.responseCode = code
		lrw.wroteHeader = true
		lrw.ResponseWriter.WriteHeader(code)
	}
}

func (lrw *LoggingResponseWriter) Write(b []byte) (int, error) {
	if !lrw.wroteHeader {
		lrw.WriteHeader(http.StatusOK)
	}

	return lrw.ResponseWriter.Write(b)
}

// Hijack implements the http.Hijacker interface to allow WebSockets to work.
func (lrw *LoggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := lrw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijack not supported")
	}

	return h.Hijack()
}
