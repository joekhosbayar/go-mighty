package router

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// Handler stubs
func CreateGame(w http.ResponseWriter, r *http.Request)       {}
func ListGames(w http.ResponseWriter, r *http.Request)        {}
func GetGame(w http.ResponseWriter, r *http.Request)          {}
func UpdateGame(w http.ResponseWriter, r *http.Request)       {}
func DeleteGame(w http.ResponseWriter, r *http.Request)       {}
func JoinGame(w http.ResponseWriter, r *http.Request)         {}
func LeaveGame(w http.ResponseWriter, r *http.Request)        {}
func ListPlayers(w http.ResponseWriter, r *http.Request)      {}
func UpdatePlayer(w http.ResponseWriter, r *http.Request)     {}
func SubmitMove(w http.ResponseWriter, r *http.Request)       {}
func GetMoves(w http.ResponseWriter, r *http.Request)         {}
func GetGameState(w http.ResponseWriter, r *http.Request)     {}
func GetGameScore(w http.ResponseWriter, r *http.Request)     {}
func GetPlayerHistory(w http.ResponseWriter, r *http.Request) {}
func RedealGame(w http.ResponseWriter, r *http.Request)       {} // optional for later

func Route() *mux.Router {
	r := mux.NewRouter()
	log.Info().Msg("Setting up endpoints")
	// Game Management
	r.HandleFunc("/games", CreateGame).Methods("POST")
	r.HandleFunc("/games", ListGames).Methods("GET")
	r.HandleFunc("/games/{gameId}", GetGame).Methods("GET")
	r.HandleFunc("/games/{gameId}", UpdateGame).Methods("PATCH")
	r.HandleFunc("/games/{gameId}", DeleteGame).Methods("DELETE")

	// Player / Session
	r.HandleFunc("/games/{gameId}/join", JoinGame).Methods("POST")
	r.HandleFunc("/games/{gameId}/leave", LeaveGame).Methods("POST")
	r.HandleFunc("/games/{gameId}/players", ListPlayers).Methods("GET")
	r.HandleFunc("/games/{gameId}/players/{playerId}", UpdatePlayer).Methods("PATCH")

	// Moves / Game Actions
	r.HandleFunc("/games/{gameId}/moves", SubmitMove).Methods("POST")
	r.HandleFunc("/games/{gameId}/moves", GetMoves).Methods("GET")
	r.HandleFunc("/games/{gameId}/state", GetGameState).Methods("GET")

	// Scoring / History
	r.HandleFunc("/games/{gameId}/score", GetGameScore).Methods("GET")
	r.HandleFunc("/players/{playerId}/history", GetPlayerHistory).Methods("GET")

	// Attach router to default HTTP handler
	http.Handle("/", r)
	return r
}
