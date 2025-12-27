package router

import (
	"net/http"
	"time"

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
