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
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

type GameService interface {
	CreateGame(ctx context.Context, id string) (*game.GameState, error)
	JoinGame(ctx context.Context, gameID, playerID, playerName string) (*game.GameState, error)
	ProcessMove(ctx context.Context, gameID, playerID string, moveType game.MoveType, payload interface{}, clientVersion int64) (*game.GameState, error)
	Subscribe(ctx context.Context, gameID string) *redis.PubSub
	GetGame(ctx context.Context, gameID string) (*game.GameState, error)
	ListGamesByStatus(ctx context.Context, status game.Phase) ([]*game.GameState, error)
}

type Handler struct {
	svc     GameService
	authSvc *service.AuthService
}

func NewHandler(svc GameService, authSvc *service.AuthService) *Handler {
	return &Handler{
		svc:     svc,
		authSvc: authSvc,
	}
}

func (h *Handler) authenticate(r *http.Request) (*service.AuthClaims, error) {
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
		return nil, errors.New("missing authentication token")
	}

	return h.authSvc.ValidateToken(tokenString)
}

func (h *Handler) authenticateWS(r *http.Request) (*service.AuthClaims, error) {
	claims, err := h.authenticate(r)
	if err == nil {
		return claims, nil
	}

	tokenString := r.URL.Query().Get("token")
	if tokenString == "" {
		return nil, errors.New("missing authentication token")
	}

	return h.authSvc.ValidateToken(tokenString)
}

// SignupHandler - POST /auth/signup
func (h *Handler) SignupHandler(w http.ResponseWriter, r *http.Request) {
	type Request struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := h.authSvc.Signup(r.Context(), req.Username, req.Password, req.Email)
	if err != nil {
		if errors.Is(err, service.ErrUserAlreadyExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(struct {
		ID        string    `json:"id"`
		Username  string    `json:"username"`
		Email     string    `json:"email"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	})
}

// LoginHandler - POST /auth/login
func (h *Handler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	type Request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	token, err := h.authSvc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// CreateGameHandler - POST /games
func (h *Handler) CreateGameHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	actualID := uuid.NewString()

	// Create the game
	g, err := h.svc.CreateGame(r.Context(), actualID)
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
	json.NewEncoder(w).Encode(updatedState)
}

// JoinGameHandler - POST /games/{id}/join
func (h *Handler) JoinGameHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

// MoveHandler - POST /games/{id}/move
func (h *Handler) MoveHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	gameID := r.PathValue("id")

	type Request struct {
		PlayerID      string        `json:"player_id"`
		MoveType      game.MoveType `json:"move_type"`
		Payload       interface{}   `json:"payload"`
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
		http.Error(w, err.Error(), http.StatusBadRequest) // Assume generic 400 for logic error
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

// ConvertPayload converts generic map/interface to concrete struct
func ConvertPayload(moveType game.MoveType, payload interface{}) (interface{}, error) {
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
		var card game.Card
		if err := json.Unmarshal(data, &card); err != nil {
			return nil, err
		}
		return card, nil
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
		return nil, fmt.Errorf("invalid play card payload: expected card or play_card_move object")
	case game.MovePass:
		return nil, nil // No payload needed for pass usually, or ignored
	default:
		return nil, nil
	}
}

// GetGameHandler - GET /games/{id}
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
	json.NewEncoder(w).Encode(g)
}

// ListGamesHandler - GET /games
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
		games = []*game.GameState{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(games)
}

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
		if statusCode >= 400 && statusCode < 500 {
			event = log.Warn()
			msg = "4xx response"
		} else if statusCode >= 500 {
			event = log.Error()
			msg = "5xx response"
		} else if statusCode >= 300 && statusCode < 400 {
			msg = "3xx redirection response"
		} else if statusCode >= 100 && statusCode < 200 {
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

type LoggingResponseWriter struct {
	http.ResponseWriter
	responseCode int
	wroteHeader  bool
}

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
