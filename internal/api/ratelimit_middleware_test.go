package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/joekhosbayar/go-mighty/internal/ratelimit"
	"github.com/joekhosbayar/go-mighty/internal/service"
	goredis "github.com/redis/go-redis/v9"
)

// newLimitedHandler builds a Handler whose limiter is backed by miniredis and
// whose validator always authenticates as userID.
func newLimitedHandler(t *testing.T, userID string) *Handler {
	t.Helper()

	mini := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	validator := &fakeValidator{claims: &service.AuthClaims{UserID: userID, Username: "alice"}}

	return NewHandler(nil, validator, WithRateLimiter(ratelimit.New(client)))
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAuthRejectsMissingToken(t *testing.T) {
	t.Parallel()

	h := newLimitedHandler(t, "user-1")
	srv := h.RequireAuth(okHandler())

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games", nil)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuthPassesClaimsToTheHandler(t *testing.T) {
	t.Parallel()

	h := newLimitedHandler(t, "user-1")

	var got string

	srv := h.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFromContext(r.Context())
		if !ok {
			t.Error("expected claims in the request context")
			return
		}

		got = claims.UserID
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games", nil)
	req.Header.Set("Authorization", "Bearer valid-test-token")
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if got != "user-1" {
		t.Fatalf("expected claims for user-1, got %q", got)
	}
}

func TestRateLimitByUserReturns429WithRetryAfter(t *testing.T) {
	t.Parallel()

	h := newLimitedHandler(t, "user-1")
	srv := h.RequireAuth(h.RateLimitByUser("creategame", ratelimit.PerHour(3))(okHandler()))

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games", nil)
		req.Header.Set("Authorization", "Bearer valid-test-token")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		return rec
	}

	for i := range 3 {
		if rec := call(); rec.Code != http.StatusOK {
			t.Fatalf("call %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	rec := call()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on the 4th call, got %d", rec.Code)
	}

	retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("expected a numeric Retry-After header, got %q", rec.Header().Get("Retry-After"))
	}

	if retry < 1 {
		t.Fatalf("expected Retry-After >= 1, got %d", retry)
	}
}

func TestRateLimitByUserIsolatesUsers(t *testing.T) {
	t.Parallel()

	mini := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	limiter := ratelimit.New(client)

	call := func(userID string) int {
		h := NewHandler(nil,
			&fakeValidator{claims: &service.AuthClaims{UserID: userID, Username: "alice"}},
			WithRateLimiter(limiter))
		srv := h.RequireAuth(h.RateLimitByUser("creategame", ratelimit.PerHour(1))(okHandler()))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/games", nil)
		req.Header.Set("Authorization", "Bearer valid-test-token")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		return rec.Code
	}

	if code := call("user-a"); code != http.StatusOK {
		t.Fatalf("user-a first call: expected 200, got %d", code)
	}

	if code := call("user-a"); code != http.StatusTooManyRequests {
		t.Fatalf("user-a second call: expected 429, got %d", code)
	}

	if code := call("user-b"); code != http.StatusOK {
		t.Fatalf("user-b must have its own budget: expected 200, got %d", code)
	}
}
