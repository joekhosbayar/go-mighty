package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/rs/zerolog/log"
)

type Handler struct {
	svc *service.GameService
}

func NewHandler(svc *service.GameService) *Handler {
	return &Handler{svc: svc}
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
	gameID := r.PathValue("id") // Go 1.22

	type Request struct {
		PlayerID string `json:"player_id"`
		Name     string `json:"name"`
		Seat     int    `json:"seat"`
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	g, err := h.svc.JoinGame(r.Context(), gameID, req.PlayerID, req.Name, req.Seat)
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

		event := log.Info()
		msg := "Success response"
		if lrw.responseCode >= 400 && lrw.responseCode < 500 {
			event = log.Warn()
			msg = "4xx response"
		} else if lrw.responseCode >= 500 {
			event = log.Error()
			msg = "5xx response"
		} else if lrw.responseCode >= 300 && lrw.responseCode < 400 {
			msg = "3xx redirection response"
		} else if lrw.responseCode >= 100 && lrw.responseCode < 200 {
			msg = "1xx informational response"
		}

		event.
			Str("method", req.Method).
			Str("url", req.URL.String()).
			Str("remote", req.RemoteAddr).
			Int("responseCode", lrw.responseCode).
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
