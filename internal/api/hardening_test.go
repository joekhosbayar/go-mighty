package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyLimitMiddlewareRejectsOversizedDeclaredBody(t *testing.T) {
	t.Parallel()

	srv := BodyLimitMiddleware(okHandler())

	body := strings.NewReader(strings.Repeat("a", int(MaxBodyBytes)+1))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games", body)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", rec.Code)
	}
}

func TestBodyLimitMiddlewarePassesNormalBodies(t *testing.T) {
	t.Parallel()

	srv := BodyLimitMiddleware(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games",
		strings.NewReader(`{"num_players":5}`))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestBodyLimitMiddlewareTruncatesUndeclaredBodies(t *testing.T) {
	t.Parallel()

	var readErr error

	srv := BodyLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, MaxBodyBytes+1024)
		for {
			_, err := r.Body.Read(buf)
			if err != nil {
				readErr = err
				break
			}
		}

		w.WriteHeader(http.StatusOK)
	}))

	// ContentLength -1 mimics a chunked upload: the declared-length check
	// can't catch it, so MaxBytesReader must.
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games",
		strings.NewReader(strings.Repeat("a", int(MaxBodyBytes)+2048)))
	req.ContentLength = -1
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if readErr == nil {
		t.Fatal("expected the body read to fail once the cap was exceeded")
	}
}
