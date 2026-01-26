package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestLoggingResponseWriter_WriteHeader tests that WriteHeader properly sets the status code
func TestLoggingResponseWriter_WriteHeader(t *testing.T) {
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

// TestLoggingResponseWriter_Write tests that Write method properly handles implicit status codes
func TestLoggingResponseWriter_Write(t *testing.T) {
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

// TestLoggingResponseWriter_MultipleWrites tests that multiple Write calls work correctly
func TestLoggingResponseWriter_MultipleWrites(t *testing.T) {
	rec := httptest.NewRecorder()
	lrw := &LoggingResponseWriter{ResponseWriter: rec}

	// First write should set implicit 200
	lrw.Write([]byte("Hello "))
	if lrw.responseCode != http.StatusOK {
		t.Errorf("Expected responseCode 200 after first write, got %d", lrw.responseCode)
	}

	// Second write should not change the status
	lrw.Write([]byte("World"))
	if lrw.responseCode != http.StatusOK {
		t.Errorf("Expected responseCode 200 after second write, got %d", lrw.responseCode)
	}

	if rec.Body.String() != "Hello World" {
		t.Errorf("Expected body 'Hello World', got %q", rec.Body.String())
	}
}

// TestLoggingMiddleware tests the logging middleware with various response scenarios
func TestLoggingMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedCode   int
		expectedBody   string
		checkDuration  bool
	}{
		{
			name: "SuccessfulResponse",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			},
			expectedCode: http.StatusOK,
			expectedBody: `{"status":"ok"}`,
		},
		{
			name: "ExplicitCreatedStatus",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte("created"))
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
				w.Write([]byte("slow"))
			},
			expectedCode:  http.StatusOK,
			expectedBody:  "slow",
			checkDuration: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test handler wrapped with logging middleware
			wrapped := LoggingMiddleware(tt.handler)

			// Create test request
			req := httptest.NewRequest(http.MethodPost, "/test", nil)
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

// TestLoggingMiddleware_RequestDetails tests that request details are properly logged
func TestLoggingMiddleware_RequestDetails(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
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
			req := httptest.NewRequest(tt.method, tt.path, tt.body)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			// Verify response is successful
			if rec.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", rec.Code)
			}
		})
	}
}

// TestLoggingMiddleware_PreservesHeaders tests that middleware preserves response headers
func TestLoggingMiddleware_PreservesHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "test-value")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	wrapped := LoggingMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
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

// TestLoggingResponseWriter_WriteHeaderOnlyOnce tests that WriteHeader only tracks the first call
func TestLoggingResponseWriter_WriteHeaderOnlyOnce(t *testing.T) {
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

// TestLoggingMiddleware_NoWriteCalls tests that middleware handles cases where neither WriteHeader nor Write is called
func TestLoggingMiddleware_NoWriteCalls(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't call WriteHeader or Write - some handlers might just set headers
		// or do nothing
	})

	wrapped := LoggingMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// When no write operations occur, status should be 200 per HTTP spec
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 for no-op handler, got %d", rec.Code)
	}
}
