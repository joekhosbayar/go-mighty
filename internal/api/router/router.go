package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/service"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// Handler holds dependencies for HTTP handlers
type Handler struct {
	gameService *service.GameService
}

// NewHandler creates a new handler instance
func NewHandler(gameService *service.GameService) *Handler {
	return &Handler{
		gameService: gameService,
	}
}

// ----------------------------
// Helper Functions
// ----------------------------

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Error().Err(err).Msg("Failed to encode JSON response")
		}
	}
}

// respondError sends an error response
func respondError(w http.ResponseWriter, status int, message string) {
	respondJSON(w, status, map[string]string{"error": message})
}

// getPlayerIDFromAuth extracts player ID from JWT or auth context
// For now, this is a stub - in production, extract from JWT claims
func getPlayerIDFromAuth(r *http.Request) string {
	// TODO: Extract from JWT Authorization header
	// For now, check X-Player-ID header for testing
	if playerID := r.Header.Get("X-Player-ID"); playerID != "" {
		return playerID
	}
	return ""
}

// ----------------------------
// Game Management Handlers
// ----------------------------

// CreateGame creates a new game
func (h *Handler) CreateGame(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GameID     string `json:"game_id"`     // Optional, will be generated if empty
		MaxPlayers int    `json:"max_players"` // Should be 5 for Mighty
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Generate game ID if not provided
	if req.GameID == "" {
		req.GameID = fmt.Sprintf("game-%d", time.Now().Unix())
	}

	// Default to 5 players for Mighty
	if req.MaxPlayers == 0 {
		req.MaxPlayers = 5
	}

	g, err := h.gameService.CreateGame(r.Context(), req.GameID, req.MaxPlayers)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create game")
		respondError(w, http.StatusInternalServerError, "Failed to create game")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"game_id":     g.GameID,
		"status":      g.Status,
		"max_players": g.MaxPlayers,
		"variant":     g.Variant,
	})
}

// JoinGame allows a player to join a game
func (h *Handler) JoinGame(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	gameID := vars["gameId"]

	playerID := getPlayerIDFromAuth(r)
	if playerID == "" {
		respondError(w, http.StatusUnauthorized, "Player ID required")
		return
	}

	var req struct {
		SeatNo int `json:"seat_no"` // 0-4 for 5-player game
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate seat number
	if req.SeatNo < 0 || req.SeatNo >= 5 {
		respondError(w, http.StatusBadRequest, "Invalid seat number (must be 0-4)")
		return
	}

	err := h.gameService.JoinGame(r.Context(), gameID, playerID, req.SeatNo)
	if err != nil {
		log.Error().Err(err).Str("game_id", gameID).Str("player_id", playerID).Msg("Failed to join game")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Successfully joined game",
		"seat_no": req.SeatNo,
	})
}

// StartGame starts a game (after all players joined)
func (h *Handler) StartGame(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	gameID := vars["gameId"]

	playerID := getPlayerIDFromAuth(r)
	if playerID == "" {
		respondError(w, http.StatusUnauthorized, "Player ID required")
		return
	}

	err := h.gameService.StartGame(r.Context(), gameID, playerID)
	if err != nil {
		log.Error().Err(err).Str("game_id", gameID).Msg("Failed to start game")
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Game started successfully",
	})
}

// GetGameState retrieves the current game state
func (h *Handler) GetGameState(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	gameID := vars["gameId"]

	playerID := getPlayerIDFromAuth(r)
	if playerID == "" {
		respondError(w, http.StatusUnauthorized, "Player ID required")
		return
	}

	snapshot, err := h.gameService.GetGameState(r.Context(), gameID, playerID)
	if err != nil {
		log.Error().Err(err).Str("game_id", gameID).Msg("Failed to get game state")
		respondError(w, http.StatusNotFound, "Game not found")
		return
	}

	respondJSON(w, http.StatusOK, snapshot)
}

// SubmitMove processes a game move
func (h *Handler) SubmitMove(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	gameID := vars["gameId"]

	playerID := getPlayerIDFromAuth(r)
	if playerID == "" {
		respondError(w, http.StatusUnauthorized, "Player ID required")
		return
	}

	var moveReq service.MoveRequest
	if err := json.NewDecoder(r.Body).Decode(&moveReq); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Override with URL params and auth
	moveReq.GameID = gameID
	moveReq.PlayerID = playerID

	response, err := h.gameService.ProcessMove(r.Context(), moveReq)
	if err != nil {
		log.Error().Err(err).Str("game_id", gameID).Str("move_type", string(moveReq.MoveType)).Msg("Failed to process move")

		// Check for version mismatch
		errMsg := err.Error()
		if errMsg == "version mismatch" || strings.HasPrefix(errMsg, "version") {
			respondError(w, http.StatusConflict, err.Error())
			return
		}

		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, response)
}

// ----------------------------
// Stub Handlers (To Be Implemented)
// ----------------------------

func (h *Handler) ListGames(w http.ResponseWriter, r *http.Request) {
	// TODO: Query Postgres for games list with filters
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) GetGame(w http.ResponseWriter, r *http.Request) {
	// TODO: Get game metadata from Postgres
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) UpdateGame(w http.ResponseWriter, r *http.Request) {
	// TODO: Update game settings (if allowed)
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) DeleteGame(w http.ResponseWriter, r *http.Request) {
	// TODO: Soft delete or archive game
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) LeaveGame(w http.ResponseWriter, r *http.Request) {
	// TODO: Allow player to leave game (before start)
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) ListPlayers(w http.ResponseWriter, r *http.Request) {
	// TODO: Get list of players in game
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) UpdatePlayer(w http.ResponseWriter, r *http.Request) {
	// TODO: Update player settings (nickname, etc.)
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) GetMoves(w http.ResponseWriter, r *http.Request) {
	// TODO: Get move history from Postgres
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) GetGameScore(w http.ResponseWriter, r *http.Request) {
	// TODO: Get current scores from game state
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) GetPlayerHistory(w http.ResponseWriter, r *http.Request) {
	// TODO: Get player's game history from Postgres
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

func (h *Handler) RedealGame(w http.ResponseWriter, r *http.Request) {
	// TODO: Redeal cards for weak hand (≤0.5 points)
	respondError(w, http.StatusNotImplemented, "Not implemented yet")
}

// ----------------------------
// Router Setup
// ----------------------------

// Route sets up all HTTP routes with the handler
func Route(gameService *service.GameService) *mux.Router {
	h := NewHandler(gameService)

	r := mux.NewRouter()
	log.Info().Msg("Setting up endpoints")

	// Game Management
	r.HandleFunc("/games", h.CreateGame).Methods("POST")
	r.HandleFunc("/games", h.ListGames).Methods("GET")
	r.HandleFunc("/games/{gameId}", h.GetGame).Methods("GET")
	r.HandleFunc("/games/{gameId}", h.UpdateGame).Methods("PATCH")
	r.HandleFunc("/games/{gameId}", h.DeleteGame).Methods("DELETE")

	// Player / Session
	r.HandleFunc("/games/{gameId}/join", h.JoinGame).Methods("POST")
	r.HandleFunc("/games/{gameId}/start", h.StartGame).Methods("POST") // Added start endpoint
	r.HandleFunc("/games/{gameId}/leave", h.LeaveGame).Methods("POST")
	r.HandleFunc("/games/{gameId}/players", h.ListPlayers).Methods("GET")
	r.HandleFunc("/games/{gameId}/players/{playerId}", h.UpdatePlayer).Methods("PATCH")

	// Moves / Game Actions
	r.HandleFunc("/games/{gameId}/moves", h.SubmitMove).Methods("POST")
	r.HandleFunc("/games/{gameId}/moves", h.GetMoves).Methods("GET")
	r.HandleFunc("/games/{gameId}/state", h.GetGameState).Methods("GET")

	// Scoring / History
	r.HandleFunc("/games/{gameId}/score", h.GetGameScore).Methods("GET")
	r.HandleFunc("/players/{playerId}/history", h.GetPlayerHistory).Methods("GET")

	r.Use(loggingMiddleware)
	return r
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		log.Info().
			Str("method", req.Method).
			Str("url", req.URL.String()).
			Str("remote", req.RemoteAddr).
			Msg("Incoming request")

		lrw := LoggingResponseWriter{w, http.StatusOK}
		start := time.Now()

		// ResponseWriter interface uses pointer receivers for its methods. Therefore, we need to pass in a pointer.
		next.ServeHTTP(&lrw, req) //Since ResponseWriter is an interface -> Interfaces internally store a pointer to a concrete value. Since request is a struct, we must specifically pass in a pointer. See ServeHTTP arguments.

		switch {
		case 100 <= lrw.responseCode && lrw.responseCode < 200:
			log.Info().
				Str("method", req.Method).
				Str("url", req.URL.String()).
				Str("remote", req.RemoteAddr).
				Int("responseCode", lrw.responseCode).
				Dur("duration", time.Since(start)).
				Msg("1xx informational response")

		case 200 <= lrw.responseCode && lrw.responseCode < 300:
			log.Info().
				Str("method", req.Method).
				Str("url", req.URL.String()).
				Str("remote", req.RemoteAddr).
				Int("responseCode", lrw.responseCode).
				Dur("duration", time.Since(start)).
				Msg("Success response")

		case 300 <= lrw.responseCode && lrw.responseCode < 400:
			log.Info().
				Str("method", req.Method).
				Str("url", req.URL.String()).
				Str("remote", req.RemoteAddr).
				Int("responseCode", lrw.responseCode).
				Dur("duration", time.Since(start)).
				Msg("3xx redirection response")

		case 400 <= lrw.responseCode && lrw.responseCode < 500:
			log.Warn().
				Str("method", req.Method).
				Str("url", req.URL.String()).
				Str("remote", req.RemoteAddr).
				Int("responseCode", lrw.responseCode).
				Dur("duration", time.Since(start)).
				Msg("4xx response")
		case 500 <= lrw.responseCode && lrw.responseCode < 600:
			log.Error().
				Str("method", req.Method).
				Str("url", req.URL.String()).
				Str("remote", req.RemoteAddr).
				Int("responseCode", lrw.responseCode).
				Dur("duration", time.Since(start)).
				Msg("5xx response")
		}
	})
}

type LoggingResponseWriter struct {
	http.ResponseWriter //embedded Type in Go. We automatically inherit all the methods from ResponseWriter
	responseCode        int
}

func (lrw *LoggingResponseWriter) WriteHeader(code int) {
	lrw.responseCode = code              //save the http response
	lrw.ResponseWriter.WriteHeader(code) //call the inherited WriteHeader
}
