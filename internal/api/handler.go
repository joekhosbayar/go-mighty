package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/rs/zerolog/log"
)

type Handler struct {
	svc     *service.GameService
	authSvc *service.AuthService
}

func NewHandler(svc *service.GameService, authSvc *service.AuthService) *Handler {
	return &Handler{
		svc:     svc,
		authSvc: authSvc,
	}
}

func (h *Handler) authenticate(r *http.Request) (*service.AuthClaims, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, errors.New("missing Authorization header")
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, errors.New("invalid Authorization header format")
	}
	return h.authSvc.ValidateToken(parts[1])
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
	json.NewEncoder(w).Encode(user)
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
	type Request struct {
		ID string `json:"id"`
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	g, err := h.svc.CreateGame(r.Context(), req.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

// JoinGameHandler - POST /games/{id}/join
func (h *Handler) JoinGameHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	gameID := r.PathValue("id")

	type Request struct {
		Seat int `json:"seat"`
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	g, err := h.svc.JoinGame(r.Context(), gameID, claims.UserID, claims.Username, req.Seat)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
}

// MoveHandler - POST /games/{id}/move
func (h *Handler) MoveHandler(w http.ResponseWriter, r *http.Request) {
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
		var card game.Card
		if err := json.Unmarshal(data, &card); err != nil {
			return nil, err
		}
		return card, nil
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
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(g)
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
