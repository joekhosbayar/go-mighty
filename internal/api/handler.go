package api

import (
	"encoding/json"
	"net/http"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
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
