package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestRoute_ReturnsMuxRouter(t *testing.T) {
	r := Route()

	if r == nil {
		t.Fatal("Route() returned nil")
	}

	if _, ok := interface{}(r).(*mux.Router); !ok {
		t.Fatalf("expected *mux.Router, got %T", r)
	}

	var _ http.Handler = r
}

func TestRoutes_MethodEnforcement(t *testing.T) {
	r := Route()

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		// Game management
		{"CreateGame", "POST", "/games", http.StatusOK},
		{"ListGames", "GET", "/games", http.StatusOK},
		{"GetGame", "GET", "/games/123", http.StatusOK},
		{"UpdateGame", "PATCH", "/games/123", http.StatusOK},
		{"DeleteGame", "DELETE", "/games/123", http.StatusOK},

		// Player/session
		{"JoinGame", "POST", "/games/123/join", http.StatusOK},
		{"LeaveGame", "POST", "/games/123/leave", http.StatusOK},
		{"ListPlayers", "GET", "/games/123/players", http.StatusOK},
		{"UpdatePlayer", "PATCH", "/games/123/players/abc", http.StatusOK},

		// Moves
		{"SubmitMove", "POST", "/games/123/moves", http.StatusOK},
		{"GetMoves", "GET", "/games/123/moves", http.StatusOK},
		{"GetGameState", "GET", "/games/123/state", http.StatusOK},

		// Scoring / history
		{"GetGameScore", "GET", "/games/123/score", http.StatusOK},
		{"GetPlayerHistory", "GET", "/players/abc/history", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()

			r.ServeHTTP(rr, req)

			if rr.Code == http.StatusMethodNotAllowed {
				t.Fatalf("route %s %s exists but method not allowed", tt.method, tt.path)
			}

			if rr.Code == http.StatusNotFound {
				t.Fatalf("route %s %s not registered", tt.method, tt.path)
			}
		})
	}
}

func TestLoggingMiddleware_CapturesStatusCode(t *testing.T) {
	called := false

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	})

	mw := loggingMiddleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	mw.ServeHTTP(rr, req)

	if !called {
		t.Fatal("expected handler to be called")
	}

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}
}

func TestLoggingResponseWriter_WriteHeader(t *testing.T) {
	rr := httptest.NewRecorder()
	lrw := &LoggingResponseWriter{
		ResponseWriter: rr,
		responseCode:   http.StatusOK,
	}

	lrw.WriteHeader(http.StatusCreated)

	if lrw.responseCode != http.StatusCreated {
		t.Fatalf("expected responseCode %d, got %d",
			http.StatusCreated, lrw.responseCode)
	}

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected underlying writer to receive %d, got %d",
			http.StatusCreated, rr.Code)
	}
}

func TestLoggingResponseWriter_DefaultStatusCode(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No WriteHeader call
		w.Write([]byte("ok"))
	})

	mw := loggingMiddleware(handler)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	mw.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected default status 200, got %d", rr.Code)
	}
}

func TestRoute_UnknownPathReturns404(t *testing.T) {
	r := Route()

	req := httptest.NewRequest("GET", "/does-not-exist", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}
