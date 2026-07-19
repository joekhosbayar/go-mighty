package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
	"github.com/joekhosbayar/go-mighty/internal/service"
	"github.com/joekhosbayar/go-mighty/internal/store/postgres"
	goredis "github.com/redis/go-redis/v9"
)

// TestLoggingResponseWriter_WriteHeader tests that WriteHeader properly sets the status code.
func TestLoggingResponseWriter_WriteHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		statusCode int
	}{
		{"OK", http.StatusOK},
		{"Created", http.StatusCreated},
		{"BadRequest", http.StatusBadRequest},
		{"NotFound", http.StatusNotFound},
		{"InternalServerError", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			lrw := &LoggingResponseWriter{ResponseWriter: rec}

			lrw.WriteHeader(tt.statusCode)

			if lrw.responseCode != tt.statusCode {
				t.Errorf("Expected responseCode %d, got %d", tt.statusCode, lrw.responseCode)
			}

			if !lrw.wroteHeader {
				t.Error("Expected wroteHeader to be true")
			}

			if rec.Code != tt.statusCode {
				t.Errorf("Expected underlying ResponseWriter code %d, got %d", tt.statusCode, rec.Code)
			}
		})
	}
}

// TestLoggingResponseWriter_Write tests that Write method properly handles implicit status codes.
func TestLoggingResponseWriter_Write(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		writeData          string
		explicitStatusCode int // 0 means no explicit WriteHeader call
		expectedCode       int
	}{
		{
			name:         "ImplicitOK",
			writeData:    `{"status":"ok"}`,
			expectedCode: http.StatusOK,
		},
		{
			name:               "ExplicitCreated",
			writeData:          `{"status":"created"}`,
			explicitStatusCode: http.StatusCreated,
			expectedCode:       http.StatusCreated,
		},
		{
			name:               "ExplicitBadRequest",
			writeData:          `{"error":"bad request"}`,
			explicitStatusCode: http.StatusBadRequest,
			expectedCode:       http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			lrw := &LoggingResponseWriter{ResponseWriter: rec}

			// Call WriteHeader explicitly if specified
			if tt.explicitStatusCode != 0 {
				lrw.WriteHeader(tt.explicitStatusCode)
			}

			// Write the data
			n, err := lrw.Write([]byte(tt.writeData))
			if err != nil {
				t.Fatalf("Write failed: %v", err)
			}

			if n != len(tt.writeData) {
				t.Errorf("Expected to write %d bytes, wrote %d", len(tt.writeData), n)
			}

			// Check status code was captured
			if lrw.responseCode != tt.expectedCode {
				t.Errorf("Expected responseCode %d, got %d", tt.expectedCode, lrw.responseCode)
			}

			if !lrw.wroteHeader {
				t.Error("Expected wroteHeader to be true after Write")
			}

			// Verify the underlying response
			if rec.Code != tt.expectedCode {
				t.Errorf("Expected underlying ResponseWriter code %d, got %d", tt.expectedCode, rec.Code)
			}

			if rec.Body.String() != tt.writeData {
				t.Errorf("Expected body %q, got %q", tt.writeData, rec.Body.String())
			}
		})
	}
}

// TestLoggingResponseWriter_MultipleWrites tests that multiple Write calls work correctly.
func TestLoggingResponseWriter_MultipleWrites(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	lrw := &LoggingResponseWriter{ResponseWriter: rec}

	// First write should set implicit 200
	_, _ = lrw.Write([]byte("Hello "))

	if lrw.responseCode != http.StatusOK {
		t.Errorf("Expected responseCode 200 after first write, got %d", lrw.responseCode)
	}

	// Second write should not change the status
	_, _ = lrw.Write([]byte("World"))

	if lrw.responseCode != http.StatusOK {
		t.Errorf("Expected responseCode 200 after second write, got %d", lrw.responseCode)
	}

	if rec.Body.String() != "Hello World" {
		t.Errorf("Expected body 'Hello World', got %q", rec.Body.String())
	}
}

// TestLoggingMiddleware tests the logging middleware with various response scenarios.
func TestLoggingMiddleware(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		handler       http.HandlerFunc
		expectedCode  int
		expectedBody  string
		checkDuration bool
	}{
		{
			name: "SuccessfulResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			},
			expectedCode: http.StatusOK,
			expectedBody: `{"status":"ok"}`,
		},
		{
			name: "ExplicitCreatedStatus",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("created"))
			},
			expectedCode: http.StatusCreated,
			expectedBody: "created",
		},
		{
			name: "BadRequestError",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "bad request", http.StatusBadRequest)
			},
			expectedCode: http.StatusBadRequest,
		},
		{
			name: "NotFoundError",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "not found", http.StatusNotFound)
			},
			expectedCode: http.StatusNotFound,
		},
		{
			name: "InternalServerError",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			},
			expectedCode: http.StatusInternalServerError,
		},
		{
			name: "RedirectResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/new-location", http.StatusFound)
			},
			expectedCode: http.StatusFound,
		},
		{
			name: "SlowResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(50 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("slow"))
			},
			expectedCode:  http.StatusOK,
			expectedBody:  "slow",
			checkDuration: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a test handler wrapped with logging middleware
			wrapped := LoggingMiddleware(tt.handler)

			// Create test request
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", nil)
			rec := httptest.NewRecorder()

			// Capture start time for duration check
			start := time.Now()

			// Execute request
			wrapped.ServeHTTP(rec, req)

			// Check status code
			if rec.Code != tt.expectedCode {
				t.Errorf("Expected status code %d, got %d", tt.expectedCode, rec.Code)
			}

			// Check body if specified
			if tt.expectedBody != "" {
				body := strings.TrimSpace(rec.Body.String())

				expectedBody := strings.TrimSpace(tt.expectedBody)
				if !strings.Contains(body, expectedBody) {
					t.Errorf("Expected body to contain %q, got %q", expectedBody, body)
				}
			}

			// Check duration if specified
			if tt.checkDuration {
				elapsed := time.Since(start)
				if elapsed < 50*time.Millisecond {
					t.Errorf("Expected duration >= 50ms, got %v", elapsed)
				}
			}
		})
	}
}

// TestLoggingMiddleware_RequestDetails tests that request details are properly logged.
func TestLoggingMiddleware_RequestDetails(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	wrapped := LoggingMiddleware(handler)

	tests := []struct {
		name   string
		method string
		path   string
		body   io.Reader
	}{
		{
			name:   "GET request",
			method: http.MethodGet,
			path:   "/games/123",
			body:   nil,
		},
		{
			name:   "POST request with body",
			method: http.MethodPost,
			path:   "/games",
			body:   bytes.NewBufferString(`{"id":"test"}`),
		},
		{
			name:   "PUT request",
			method: http.MethodPut,
			path:   "/games/456/move",
			body:   bytes.NewBufferString(`{"action":"play"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, tt.body)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			// Verify response is successful
			if rec.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", rec.Code)
			}
		})
	}
}

// TestLoggingMiddleware_PreservesHeaders tests that middleware preserves response headers.
func TestLoggingMiddleware_PreservesHeaders(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "test-value")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	wrapped := LoggingMiddleware(handler)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Check headers are preserved
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type header not preserved")
	}

	if rec.Header().Get("X-Custom-Header") != "test-value" {
		t.Errorf("Custom header not preserved")
	}
}

// TestLoggingResponseWriter_WriteHeaderOnlyOnce tests that WriteHeader only tracks the first call.
func TestLoggingResponseWriter_WriteHeaderOnlyOnce(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	lrw := &LoggingResponseWriter{ResponseWriter: rec}

	// First call
	lrw.WriteHeader(http.StatusOK)

	if lrw.responseCode != http.StatusOK {
		t.Errorf("Expected responseCode 200, got %d", lrw.responseCode)
	}

	// Second call should be ignored by both the wrapper and underlying ResponseWriter
	lrw.WriteHeader(http.StatusBadRequest)

	// The wrapper should still have the first status code
	if lrw.responseCode != http.StatusOK {
		t.Errorf("Expected responseCode to remain 200, got %d", lrw.responseCode)
	}

	// The underlying ResponseWriter should also still have the first status
	if rec.Code != http.StatusOK {
		t.Errorf("Expected underlying code 200, got %d", rec.Code)
	}
}

// TestLoggingMiddleware_NoWriteCalls tests that middleware handles cases where neither WriteHeader nor Write is called.
func TestLoggingMiddleware_NoWriteCalls(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't call WriteHeader or Write - some handlers might just set headers
		// or do nothing
	})

	wrapped := LoggingMiddleware(handler)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// When no write operations occur, status should be 200 per HTTP spec
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 for no-op handler, got %d", rec.Code)
	}
}

// TestLoggingResponseWriter_Hijack tests that Hijack properly delegates to underlying ResponseWriter.
func TestLoggingResponseWriter_Hijack(t *testing.T) {
	t.Parallel()
	t.Run("Hijack not supported", func(t *testing.T) {
		t.Parallel()
		// httptest.ResponseRecorder does not support hijacking
		rec := httptest.NewRecorder()
		lrw := &LoggingResponseWriter{ResponseWriter: rec}

		_, _, err := lrw.Hijack()
		if err == nil {
			t.Error("Expected error when Hijack is not supported")
		}

		if err.Error() != "hijack not supported" {
			t.Errorf("Expected 'hijack not supported' error, got %v", err)
		}
	})

	t.Run("Hijack supported", func(t *testing.T) {
		t.Parallel()
		// Create a mock ResponseWriter that implements http.Hijacker
		mockHijacker := &mockHijackerResponseWriter{}
		lrw := &LoggingResponseWriter{ResponseWriter: mockHijacker}

		conn, rw, err := lrw.Hijack()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if !mockHijacker.hijackCalled {
			t.Error("Expected Hijack to be called on underlying ResponseWriter")
		}

		if conn == nil {
			t.Error("Expected non-nil connection")
		}

		if rw == nil {
			t.Error("Expected non-nil ReadWriter")
		}
	})
}

// mockHijackerResponseWriter is a mock ResponseWriter that implements http.Hijacker.
type mockHijackerResponseWriter struct {
	hijackCalled bool
}

func (m *mockHijackerResponseWriter) Header() http.Header {
	return http.Header{}
}

func (m *mockHijackerResponseWriter) Write([]byte) (int, error) {
	return 0, nil
}

func (m *mockHijackerResponseWriter) WriteHeader(_ int) {}

func (m *mockHijackerResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	m.hijackCalled = true
	// Return mock values
	return &mockConn{}, bufio.NewReadWriter(bufio.NewReader(nil), bufio.NewWriter(nil)), nil
}

// mockConn is a minimal implementation of net.Conn for testing.
type mockConn struct{}

func (m *mockConn) Read(_ []byte) (n int, err error)   { return 0, nil }
func (m *mockConn) Write(_ []byte) (n int, err error)  { return 0, nil }
func (m *mockConn) Close() error                       { return nil }
func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(_ time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(_ time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(_ time.Time) error { return nil }

// busyGameService fails every ProcessMove with lock contention.
type busyGameService struct{}

func (busyGameService) CreateGame(_ context.Context, _ string, _ game.GameConfig) (*game.Game, error) {
	return nil, nil
}
func (busyGameService) JoinGame(_ context.Context, _, _, _ string) (*game.Game, error) {
	return nil, service.ErrGameBusy
}
func (busyGameService) ProcessMove(_ context.Context, _, _ string, _ game.MoveType, _ any, _ int64) (*game.Game, error) {
	return nil, service.ErrGameBusy
}
func (busyGameService) Subscribe(_ context.Context, _ string) *goredis.PubSub   { return nil }
func (busyGameService) GetGame(_ context.Context, _ string) (*game.Game, error) { return nil, nil }
func (busyGameService) ListGamesByStatus(_ context.Context, _ game.Phase) ([]*game.Game, error) {
	return nil, nil
}

func TestMoveHandlerMapsGameBusyTo409(t *testing.T) {
	t.Parallel()

	h := NewHandler(busyGameService{}, service.NewAuth(&postgres.Store{}, "testsecret"))

	req := httptest.NewRequest(http.MethodPost, "/games/g1/move",
		strings.NewReader(`{"move_type":"pass","client_version":1,"payload":null}`))
	req.SetPathValue("id", "g1")
	req.Header.Set("Authorization", "Bearer "+generateValidToken("user-1", "alice"))

	rec := httptest.NewRecorder()
	h.MoveHandler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestJoinHandlerMapsGameBusyTo409(t *testing.T) {
	t.Parallel()

	h := NewHandler(busyGameService{}, service.NewAuth(&postgres.Store{}, "testsecret"))

	req := httptest.NewRequest(http.MethodPost, "/games/g1/join", nil)
	req.SetPathValue("id", "g1")
	req.Header.Set("Authorization", "Bearer "+generateValidToken("user-1", "alice"))

	rec := httptest.NewRecorder()
	h.JoinGameHandler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestConvertPayloadCallPartnerShapes(t *testing.T) {
	t.Parallel()

	got, err := ConvertPayload(game.MoveCallPartner, map[string]any{"no_friend": true})
	if err != nil {
		t.Fatalf("no_friend: %v", err)
	}

	if move, ok := got.(game.CallPartnerMove); !ok || !move.NoFriend || move.Card != nil {
		t.Fatalf("bad no_friend conversion: %#v", got)
	}

	got, err = ConvertPayload(game.MoveCallPartner, map[string]any{"card": map[string]any{"suit": "hearts", "rank": "A"}})
	if err != nil {
		t.Fatalf("card shape: %v", err)
	}

	if move, ok := got.(game.CallPartnerMove); !ok || move.Card == nil || move.Card.Suit != game.Hearts {
		t.Fatalf("bad card conversion: %#v", got)
	}

	got, err = ConvertPayload(game.MoveCallPartner, map[string]any{"suit": "hearts", "rank": "A"})
	if err != nil {
		t.Fatalf("legacy shape: %v", err)
	}

	if move, ok := got.(game.CallPartnerMove); !ok || move.Card == nil || move.Card.Rank != game.Ace {
		t.Fatalf("bad legacy conversion: %#v", got)
	}

	if _, err := ConvertPayload(game.MoveCallPartner, map[string]any{}); err == nil {
		t.Fatal("empty call_partner payload must be rejected")
	}
}
