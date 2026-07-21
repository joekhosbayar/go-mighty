# Safeguards: Rate Limiting & Hardening Implementation Plan (Plan 4 of 5)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the spec's layered abuse controls — a custom Caddy build doing per-IP HTTP rate limiting plus TLS/CORS/header hygiene at the edge, and Go middleware doing per-user, per-connection, and per-IP limits inside the app — without changing any game behaviour.

**Architecture:** Two independent layers. **Edge (Caddy):** a custom `xcaddy` image with `mholt/caddy-ratelimit`, published to its own ECR repo and pulled by the prod compose stack; it enforces per-IP request zones, a 64KB body cap, security headers, and a CORS allowlist. **App (Go):** a new `internal/ratelimit` package with a Redis token bucket (per-user/per-action, survives restarts) and an in-process bucket (per-connection message rate); `internal/api` gains an auth-context middleware so a rate-limit middleware can key on the Cognito `sub`, plus WebSocket hardening — origin allowlist, frame size cap, idle timeout, message rate cap, and concurrent-connection caps.

**Tech Stack:** Go 1.25, `redis/go-redis/v9` (Lua `EVALSHA` token bucket), `alicebob/miniredis/v2` (tests), `gorilla/websocket`, Caddy 2 + `xcaddy` + `mholt/caddy-ratelimit`, Docker buildx (linux/arm64), OpenTofu, AWS CLI v2.

**Spec:** `docs/superpowers/specs/2026-07-18-aws-mvp-architecture-design.md`, Section 3 (Layers 1 and 2; Layer 0/0.5 are automatic, Layer 3 shipped in Plan 1). Out of scope: OTel/Grafana, GitHub Actions CI/CD (Plan 5); the Cloudflare escalation runbook (documented only, built only if attacked).

## Global Constraints

- Region: **us-east-1** for every AWS resource and CLI call.
- Compute: **t4g.small (ARM64/Graviton)** — every image must be built and pushed as **linux/arm64**.
- Domain: **`themighty.gg`**. API is `api.themighty.gg`. The SPA is served by Amplify at the **apex and `www`** (`deploy/terraform/amplify.tf`), *not* at `app.<domain>` as the spec prose says — so every allowlist in this plan uses `https://themighty.gg` and `https://www.themighty.gg`.
- Security group inbound stays **443 and 80 only**. **No SSH ever** — deploys and shell access go through SSM.
- Postgres and Redis stay on the compose network with **no host port mappings** in prod.
- Limits from the spec, used verbatim: general edge zone **100 req/min/IP**; strict edge zone **10 req/min/IP**; body cap **64KB**; game creation **10/hour/user**; WebSocket messages **10/sec, burst 20, per connection**; concurrent sockets **3/user, 20/IP**; AUTH handshake deadline stays **5 seconds**.
- Never set `ReadTimeout` or `WriteTimeout` on the Go `http.Server`, and never set Caddy's `read_body`/`write`/`idle` server timeouts — all of them break long-lived WebSocket connections. Only header-read timeouts are safe.
- Rate limiting **fails open**: if Redis is unreachable the request is allowed and a warning is logged. Locking every player out during a Redis blip is worse than the abuse being limited.
- All local paths are relative to the `go-mighty` repo root. Run `gofmt -l .` (expect empty output) and `go vet ./...` before every commit.
- Test command for the Go tasks: `go test ./internal/...`. The `api`, `ratelimit`, and `store/redis` packages run under `goleak` via their `TestMain` — every test must close the clients and connections it opens.

---

### Task 1: Redis token-bucket limiter (`internal/ratelimit`)

The shared primitive for every per-user limit. A Lua script makes read-modify-write atomic across concurrent requests; the clock is injected so tests are deterministic.

**Files:**
- Create: `internal/ratelimit/limiter.go`
- Create: `internal/ratelimit/limiter_test.go`
- Create: `internal/ratelimit/main_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `ratelimit.Rule{Capacity, RefillPerSec float64}`, `ratelimit.PerHour(n float64) Rule`, `ratelimit.Decision{Allowed bool, RetryAfter time.Duration}`, `ratelimit.New(client redis.Scripter) *Limiter`, `ratelimit.NewWithClock(client redis.Scripter, now func() time.Time) *Limiter`, and `(*Limiter).Allow(ctx context.Context, key string, rule Rule) Decision`. Task 2 consumes all of these.

- [ ] **Step 1: Write the goleak TestMain**

Create `internal/ratelimit/main_test.go`:

```go
package ratelimit

import (
	"os"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
	os.Exit(m.Run())
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/ratelimit/limiter_test.go`:

```go
package ratelimit

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestLimiter returns a Limiter backed by miniredis whose clock is driven
// by the returned pointer, so refill behaviour can be tested without sleeping.
func newTestLimiter(t *testing.T) (*Limiter, *time.Time) {
	t.Helper()

	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	return NewWithClock(client, func() time.Time { return now }), &now
}

func TestAllowConsumesCapacityThenDenies(t *testing.T) {
	limiter, _ := newTestLimiter(t)
	rule := Rule{Capacity: 3, RefillPerSec: 0.001}

	for i := range 3 {
		if d := limiter.Allow(t.Context(), "k", rule); !d.Allowed {
			t.Fatalf("call %d: expected allowed, got denied", i+1)
		}
	}

	d := limiter.Allow(t.Context(), "k", rule)
	if d.Allowed {
		t.Fatal("expected the 4th call to be denied")
	}

	if d.RetryAfter <= 0 {
		t.Fatalf("expected a positive RetryAfter, got %v", d.RetryAfter)
	}
}

func TestAllowRefillsOverTime(t *testing.T) {
	limiter, clock := newTestLimiter(t)
	rule := Rule{Capacity: 2, RefillPerSec: 1}

	limiter.Allow(t.Context(), "k", rule)
	limiter.Allow(t.Context(), "k", rule)

	if d := limiter.Allow(t.Context(), "k", rule); d.Allowed {
		t.Fatal("expected the bucket to be empty")
	}

	*clock = clock.Add(2 * time.Second)

	if d := limiter.Allow(t.Context(), "k", rule); !d.Allowed {
		t.Fatal("expected the bucket to have refilled after 2s")
	}
}

func TestAllowNeverExceedsCapacityOnRefill(t *testing.T) {
	limiter, clock := newTestLimiter(t)
	rule := Rule{Capacity: 2, RefillPerSec: 1}

	limiter.Allow(t.Context(), "k", rule)
	*clock = clock.Add(1 * time.Hour)

	// An hour of refill must still cap at 2 tokens, not 3600.
	limiter.Allow(t.Context(), "k", rule)
	limiter.Allow(t.Context(), "k", rule)

	if d := limiter.Allow(t.Context(), "k", rule); d.Allowed {
		t.Fatal("expected capacity to cap refill at 2 tokens")
	}
}

func TestAllowKeysAreIndependent(t *testing.T) {
	limiter, _ := newTestLimiter(t)
	rule := Rule{Capacity: 1, RefillPerSec: 0.001}

	if d := limiter.Allow(t.Context(), "user-a", rule); !d.Allowed {
		t.Fatal("user-a should be allowed")
	}

	if d := limiter.Allow(t.Context(), "user-a", rule); d.Allowed {
		t.Fatal("user-a should be exhausted")
	}

	if d := limiter.Allow(t.Context(), "user-b", rule); !d.Allowed {
		t.Fatal("user-b must have its own bucket")
	}
}

func TestAllowFailsOpenWhenRedisIsUnavailable(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	_ = client.Close() // force every command to error

	limiter := New(client)

	if d := limiter.Allow(t.Context(), "k", Rule{Capacity: 1, RefillPerSec: 1}); !d.Allowed {
		t.Fatal("limiter must fail open when Redis is unavailable")
	}
}

func TestPerHourBuildsAFullDayBurstFreeRule(t *testing.T) {
	rule := PerHour(10)

	if rule.Capacity != 10 {
		t.Fatalf("expected capacity 10, got %v", rule.Capacity)
	}

	want := 10.0 / 3600.0
	if rule.RefillPerSec != want {
		t.Fatalf("expected refill %v, got %v", want, rule.RefillPerSec)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/ratelimit/ -v`
Expected: FAIL — `undefined: Limiter`, `undefined: Rule`, `undefined: New`, `undefined: NewWithClock`, `undefined: PerHour`.

- [ ] **Step 4: Write the implementation**

Create `internal/ratelimit/limiter.go`:

```go
// Package ratelimit provides the rate limiters used by the API layer: a
// Redis-backed token bucket for per-user/per-action limits that must hold
// across process restarts and (later) across instances, and an in-process
// bucket for per-connection limits that need neither.
package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// Rule describes one token bucket: Capacity tokens of burst, refilled at
// RefillPerSec tokens per second.
type Rule struct {
	Capacity     float64
	RefillPerSec float64
}

// PerHour builds a Rule permitting n events per hour, where the whole hourly
// budget may be spent as a single burst (spec Section 3: "game creation
// ~10/hour/user").
func PerHour(n float64) Rule {
	return Rule{Capacity: n, RefillPerSec: n / 3600}
}

// Decision is the outcome of one Allow call. RetryAfter is only meaningful
// when Allowed is false.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// bucketScript is a token bucket evaluated server-side so that the
// read-refill-write sequence is atomic across concurrent requests. The bucket
// is stored as a hash of {tokens, ts} and expires once it would be full
// again, so idle users cost nothing.
//
// KEYS[1] bucket key. ARGV: capacity, refill/sec, now (ms), cost.
// Returns {allowed, retry_after_ms}.
var bucketScript = redis.NewScript(`
local capacity = tonumber(ARGV[1])
local refill   = tonumber(ARGV[2])
local now_ms   = tonumber(ARGV[3])
local cost     = tonumber(ARGV[4])

local data   = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts     = tonumber(data[2])

if tokens == nil or ts == nil then
  tokens = capacity
  ts = now_ms
end

local elapsed = (now_ms - ts) / 1000.0
if elapsed > 0 then
  tokens = math.min(capacity, tokens + elapsed * refill)
end

local allowed = 0
local retry_ms = 0
if tokens >= cost then
  allowed = 1
  tokens = tokens - cost
else
  retry_ms = math.ceil(((cost - tokens) / refill) * 1000)
end

redis.call('HSET', KEYS[1], 'tokens', tokens, 'ts', now_ms)
redis.call('PEXPIRE', KEYS[1], math.ceil((capacity / refill) * 1000) + 1000)

return {allowed, retry_ms}
`)

// Limiter is a Redis-backed token bucket. It is safe for concurrent use.
type Limiter struct {
	client redis.Scripter
	now    func() time.Time
}

// New returns a Limiter using the real clock.
func New(client redis.Scripter) *Limiter {
	return NewWithClock(client, time.Now)
}

// NewWithClock returns a Limiter with an injected clock, for tests.
func NewWithClock(client redis.Scripter, now func() time.Time) *Limiter {
	return &Limiter{client: client, now: now}
}

// Allow consumes one token from the bucket at key.
//
// It fails open: a nil limiter, a nonsensical rule, or an unreachable Redis
// all return Allowed. A rate limiter that locks every player out of the game
// during a Redis blip is worse than the abuse it prevents, and the outage
// itself is separately alarmed on.
func (l *Limiter) Allow(ctx context.Context, key string, rule Rule) Decision {
	if l == nil || l.client == nil || rule.RefillPerSec <= 0 || rule.Capacity <= 0 {
		return Decision{Allowed: true}
	}

	res, err := bucketScript.Run(ctx, l.client, []string{key},
		rule.Capacity, rule.RefillPerSec, l.now().UnixMilli(), 1).Slice()
	if err != nil {
		log.Warn().Str("key", key).Err(err).Msg("Rate limiter unavailable, allowing request")
		return Decision{Allowed: true}
	}

	if len(res) != 2 {
		log.Warn().Str("key", key).Int("len", len(res)).Msg("Unexpected rate limiter reply, allowing request")
		return Decision{Allowed: true}
	}

	allowed, _ := res[0].(int64)
	retryMS, _ := res[1].(int64)

	return Decision{
		Allowed:    allowed == 1,
		RetryAfter: time.Duration(retryMS) * time.Millisecond,
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ratelimit/ -v`
Expected: PASS — all seven tests (six `TestAllow*`/`TestPerHour*` plus goleak's clean exit).

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./internal/ratelimit/
git add internal/ratelimit
git commit -m "feat(ratelimit): add Redis token-bucket limiter"
```

---

### Task 2: Auth context + per-user game-creation limit

The API authenticates inside each handler, so a middleware has no way to see the caller. This task adds an auth middleware that stashes claims in the request context and makes `authenticate` prefer them — additive, so existing handlers and their tests are untouched — then layers the per-user limiter on `POST /games`.

**Files:**
- Create: `internal/api/options.go`
- Create: `internal/api/authctx.go`
- Create: `internal/api/ratelimit_middleware.go`
- Create: `internal/api/ratelimit_middleware_test.go`
- Modify: `internal/api/handler.go:37-70` (Handler struct, `NewHandler`, `authenticate`)
- Modify: `cmd/server/main.go:63-104`

**Interfaces:**
- Consumes: `ratelimit.New`, `ratelimit.PerHour`, `ratelimit.Rule`, `(*Limiter).Allow` from Task 1.
- Produces: `api.Option` (a `func(*Handler)`), `api.WithRateLimiter(*ratelimit.Limiter) Option`, `api.NewHandler(svc GameService, authSvc TokenValidator, opts ...Option) *Handler`, `api.WithClaims(ctx, *service.AuthClaims) context.Context`, `api.ClaimsFromContext(ctx) (*service.AuthClaims, bool)`, `(*Handler).RequireAuth(next http.Handler) http.Handler`, `(*Handler).RateLimitByUser(action string, rule ratelimit.Rule) func(http.Handler) http.Handler`. Tasks 4, 5, and 6 add further `Option` constructors to `options.go`.

- [ ] **Step 1: Write the failing tests**

Create `internal/api/ratelimit_middleware_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestRequireAuth|TestRateLimitByUser' -v`
Expected: FAIL — `undefined: WithRateLimiter`, `undefined: ClaimsFromContext`, `h.RequireAuth undefined`, `h.RateLimitByUser undefined`.

- [ ] **Step 3: Add the options file**

Create `internal/api/options.go`:

```go
package api

import "github.com/joekhosbayar/go-mighty/internal/ratelimit"

// Option configures a Handler at construction time. Options exist so that
// production wiring (rate limits, origin allowlists, connection caps) can be
// injected from main without changing the NewHandler signature every time a
// safeguard is added.
type Option func(*Handler)

// WithRateLimiter installs the Redis-backed limiter used by RateLimitByUser.
// Without it, per-user limits are disabled (the local dev default).
func WithRateLimiter(l *ratelimit.Limiter) Option {
	return func(h *Handler) { h.limiter = l }
}
```

- [ ] **Step 4: Add the auth context helpers**

Create `internal/api/authctx.go`:

```go
package api

import (
	"context"
	"net/http"

	"github.com/joekhosbayar/go-mighty/internal/service"
)

// claimsCtxKey is unexported so nothing outside this package can collide with
// or forge the context value.
type claimsCtxKey struct{}

// WithClaims returns a copy of ctx carrying the authenticated identity.
func WithClaims(ctx context.Context, claims *service.AuthClaims) context.Context {
	return context.WithValue(ctx, claimsCtxKey{}, claims)
}

// ClaimsFromContext returns the identity stored by RequireAuth, if any.
func ClaimsFromContext(ctx context.Context) (*service.AuthClaims, bool) {
	claims, ok := ctx.Value(claimsCtxKey{}).(*service.AuthClaims)
	return claims, ok && claims != nil
}

// RequireAuth validates the bearer token once, up front, and stores the
// resulting claims in the request context. Middleware layered beneath it
// (rate limiting) can then key on the Cognito sub without re-validating, and
// handlers keep working unchanged because authenticate reads the context
// first.
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := h.authenticate(r)
		if err != nil {
			writeAuthError(w, err)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}
```

- [ ] **Step 5: Add the rate-limit middleware**

Create `internal/api/ratelimit_middleware.go`:

```go
package api

import (
	"math"
	"net/http"
	"strconv"

	"github.com/joekhosbayar/go-mighty/internal/ratelimit"
	"github.com/rs/zerolog/log"
)

// RateLimitByUser limits one action per authenticated user (spec Section 3,
// Layer 2). It must be layered inside RequireAuth; without claims in the
// context there is nothing to key on and the request passes through, because
// the edge per-IP zone still applies.
func (h *Handler) RateLimitByUser(action string, rule ratelimit.Rule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok || h.limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			decision := h.limiter.Allow(r.Context(), "rl:"+action+":"+claims.UserID, rule)
			if !decision.Allowed {
				retry := int(math.Ceil(decision.RetryAfter.Seconds()))
				if retry < 1 {
					retry = 1
				}

				log.Warn().
					Str("action", action).
					Str("user_id", claims.UserID).
					Int("retry_after_s", retry).
					Msg("Per-user rate limit exceeded")

				w.Header().Set("Retry-After", strconv.Itoa(retry))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 6: Make Handler accept options and prefer context claims**

In `internal/api/handler.go`, replace the `Handler` struct and `NewHandler` (lines 36-48) with:

```go
// Handler handles HTTP requests for the game API.
type Handler struct {
	svc     GameService
	authSvc TokenValidator
	limiter *ratelimit.Limiter
}

// NewHandler creates a new Handler with the given services. Options carry the
// production safeguards (rate limiting, origin allowlist, connection caps);
// with none supplied the handler behaves exactly as it did before they were
// added, which keeps local dev and the existing tests simple.
func NewHandler(svc GameService, authSvc TokenValidator, opts ...Option) *Handler {
	h := &Handler{
		svc:     svc,
		authSvc: authSvc,
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}
```

Add `"github.com/joekhosbayar/go-mighty/internal/ratelimit"` to the import block in `internal/api/handler.go`.

Then, in the same file, insert the context fast path as the first statement of `authenticate` (currently line 50):

```go
func (h *Handler) authenticate(r *http.Request) (*service.AuthClaims, error) {
	// RequireAuth already validated this request; don't pay for a second
	// JWKS verification just because the handler still calls authenticate.
	if claims, ok := ClaimsFromContext(r.Context()); ok {
		return claims, nil
	}

	if h.authSvc == nil {
		return nil, errors.New("authentication service is not configured")
	}
	// ...rest unchanged
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/api/ -run 'TestRequireAuth|TestRateLimitByUser' -v`
Expected: PASS — 4 tests.

Run: `go test ./internal/...`
Expected: PASS — the pre-existing api tests still pass, since `NewHandler`'s two-argument form is unchanged.

- [ ] **Step 8: Wire it in main**

In `cmd/server/main.go`, add `goredis "github.com/redis/go-redis/v9"` and `"github.com/joekhosbayar/go-mighty/internal/ratelimit"` to the imports.

After `redisStore := redis.NewStore(redisAddr)` (line 63), add:

```go
	// A separate client for the limiter: the game store's client is private
	// to that package, and one extra small pool is cheaper than widening its
	// API surface.
	rlClient := goredis.NewClient(&goredis.Options{Addr: redisAddr})
	defer func() { _ = rlClient.Close() }()

	limiter := ratelimit.New(rlClient)
```

Replace line 94 (`handler := api.NewHandler(svc, authSvc)`) with:

```go
	handler := api.NewHandler(svc, authSvc, api.WithRateLimiter(limiter))
```

Replace the `POST /games` route (line 99) with:

```go
	mux.Handle("POST /games", handler.RequireAuth(
		handler.RateLimitByUser("creategame", ratelimit.PerHour(10))(
			http.HandlerFunc(handler.CreateGameHandler))))
```

- [ ] **Step 9: Verify the server still builds and serves**

Run: `go build ./... && docker compose up -d --build`
Then: `curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/games`
Expected: `200`

Then confirm the limit is live (11th create is refused — the first ten need a valid token, so an unauthenticated probe should still be 401, proving RequireAuth runs before the limiter):
Run: `curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/games`
Expected: `401`

Run: `docker compose down`

- [ ] **Step 10: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/api cmd/server/main.go
git commit -m "feat(api): per-user rate limiting on game creation"
```

---

### Task 3: HTTP body cap and server timeouts

Caddy caps bodies at the edge, but the app must not depend on the edge being there — the same limits belong in Go so local dev, tests, and any future ingress inherit them.

**Files:**
- Create: `internal/api/hardening.go`
- Create: `internal/api/hardening_test.go`
- Modify: `cmd/server/main.go:106-116`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `api.MaxBodyBytes` (int64 constant, 65536) and `api.BodyLimitMiddleware(next http.Handler) http.Handler`.

- [ ] **Step 1: Write the failing tests**

Create `internal/api/hardening_test.go`:

```go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api/ -run TestBodyLimitMiddleware -v`
Expected: FAIL — `undefined: BodyLimitMiddleware`, `undefined: MaxBodyBytes`.

- [ ] **Step 3: Write the implementation**

Create `internal/api/hardening.go`:

```go
package api

import (
	"net/http"
)

// MaxBodyBytes caps request bodies at 64KB (spec Section 3, Layer 1). No
// legitimate request to this API is anywhere near that: the largest is a
// game-config change of a few hundred bytes.
const MaxBodyBytes int64 = 64 << 10

// BodyLimitMiddleware enforces MaxBodyBytes twice over: a declared
// Content-Length above the cap is refused outright with 413, and anything
// else is wrapped so a chunked or lying client still can't stream more than
// the cap into memory.
//
// It is safe on WebSocket upgrades: those carry no body, and wrapping
// r.Body does not interfere with hijacking the connection.
func BodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > MaxBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
		}

		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/api/ -run TestBodyLimitMiddleware -v`
Expected: PASS — 3 tests.

- [ ] **Step 5: Replace ListenAndServe with a configured server**

In `cmd/server/main.go`, replace the final block (lines 112-116) with:

```go
	log.Printf("Server starting on port %s", port)

	// ReadTimeout and WriteTimeout are deliberately unset: both apply to
	// hijacked connections and would kill long-lived WebSockets mid-game.
	// ReadHeaderTimeout is the safe one — it bounds slowloris-style header
	// stalls without touching an established socket.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           api.LoggingMiddleware(api.BodyLimitMiddleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
```

(`time` and `net/http` are already imported.)

- [ ] **Step 6: Verify end to end**

Run: `go build ./... && docker compose up -d --build`
Then:

```bash
python3 -c "print('{\"num_players\":5,\"pad\":\"' + 'a'*70000 + '\"}')" > /tmp/big.json
curl -s -o /dev/null -w "%{http_code}\n" -X POST -H 'Content-Type: application/json' \
  --data-binary @/tmp/big.json http://localhost:8080/games
```

Expected: `413`

Run: `docker compose down`

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/api cmd/server/main.go
git commit -m "feat(api): cap request bodies at 64KB and bound header reads"
```

---

### Task 4: WebSocket origin allowlist, frame cap, and idle timeout

The current origin check requires the Origin host to equal the request host — which is false in production (the SPA is on `themighty.gg`, the API on `api.themighty.gg`), so this is a launch blocker as well as a hardening task.

**Files:**
- Modify: `internal/api/options.go`
- Modify: `internal/api/handler.go` (Handler struct + `NewHandler`)
- Modify: `internal/api/ws.go:23-42` (upgrader), `:114-115` (post-auth deadline), `:163-171` (read loop)
- Create: `internal/api/ws_hardening_test.go`
- Modify: `cmd/server/main.go` (read `ALLOWED_ORIGINS`)

**Interfaces:**
- Consumes: `api.Option` from Task 2.
- Produces: `api.WithAllowedOrigins(origins []string) Option`, and the constants `maxWSMessageBytes` (int64, 32768) and `wsIdleTimeout` (60s), both unexported and used by Tasks 5 and 6.

- [ ] **Step 1: Write the failing tests**

Create `internal/api/ws_hardening_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// serveWS mounts handler on a test server at the websocket route.
func serveWS(t *testing.T, handler *Handler) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/games/{id}/ws", handler.WSHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return server
}

// dialWSWithOrigin dials without the auth step, returning the handshake error
// and response so origin rejection can be asserted.
func dialWSWithOrigin(t *testing.T, server *httptest.Server, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/games/game-1/ws"

	header := http.Header{}
	header.Set("Origin", origin)

	conn, resp, err := websocket.DefaultDialer.DialContext(t.Context(), wsURL, header)
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}

	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}

	return conn, resp, err
}

func TestWSHandlerRejectsDisallowedOrigin(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithAllowedOrigins([]string{"https://themighty.gg"})(handler)

	server := serveWS(t, handler)

	_, resp, err := dialWSWithOrigin(t, server, "https://evil.example")
	if err == nil {
		t.Fatal("expected the handshake to be rejected")
	}

	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %+v", resp)
	}
}

func TestWSHandlerAcceptsAllowedOrigin(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithAllowedOrigins([]string{"https://themighty.gg", "https://www.themighty.gg"})(handler)

	server := serveWS(t, handler)

	conn, _, err := dialWSWithOrigin(t, server, "https://www.themighty.gg")
	if err != nil {
		t.Fatalf("expected the handshake to succeed, got %v", err)
	}

	if conn == nil {
		t.Fatal("expected a connection")
	}
}

func TestWSHandlerAllowsCrossOriginlessClients(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithAllowedOrigins([]string{"https://themighty.gg"})(handler)

	server := serveWS(t, handler)

	// Native clients (the Swift app) send no Origin at all; they authenticate
	// with a token instead, so the origin check must not block them.
	_, _, err := dialWSWithOrigin(t, server, "")
	if err != nil {
		t.Fatalf("expected an Origin-less handshake to succeed, got %v", err)
	}
}

func TestWSHandlerClosesConnectionOnOversizedFrame(t *testing.T) {
	t.Parallel()

	server, _ := setupWSTestServer(t)
	conn := dialWS(t, server, "/games/game-1/ws", generateValidToken("user-1", "alice"))

	oversized := strings.Repeat("a", int(maxWSMessageBytes)+1024)
	if err := conn.WriteText(oversized); err != nil {
		t.Fatalf("failed to write oversized frame: %v", err)
	}

	if _, _, err := conn.Conn.ReadMessage(); err == nil {
		t.Fatal("expected the connection to be closed after an oversized frame")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api/ -run TestWSHandler -v`
Expected: FAIL — `undefined: WithAllowedOrigins`, `undefined: maxWSMessageBytes`.

- [ ] **Step 3: Add the option and the Handler fields**

Append to `internal/api/options.go`:

```go
// WithAllowedOrigins restricts WebSocket upgrades to these exact origins
// (scheme + host, e.g. "https://themighty.gg"). With none set, the handler
// falls back to a same-host check, which is what local dev wants. In
// production the SPA and the API live on different hosts, so the allowlist is
// mandatory there.
func WithAllowedOrigins(origins []string) Option {
	return func(h *Handler) {
		normalized := make([]string, 0, len(origins))

		for _, o := range origins {
			o = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(o, "/")))
			if o != "" {
				normalized = append(normalized, o)
			}
		}

		h.allowedOrigins = normalized
	}
}
```

Add `"strings"` to the imports of `internal/api/options.go`.

In `internal/api/handler.go`, extend the `Handler` struct:

```go
type Handler struct {
	svc            GameService
	authSvc        TokenValidator
	limiter        *ratelimit.Limiter
	allowedOrigins []string
}
```

- [ ] **Step 4: Replace the package-level upgrader with a handler-scoped one**

In `internal/api/ws.go`, delete the `var upgrader = websocket.Upgrader{...}` block (lines 23-42) and replace it with:

```go
const (
	// maxWSMessageBytes caps a single inbound frame. The largest legitimate
	// client message is a move of a few hundred bytes; 32KB is generous
	// headroom that still makes memory exhaustion via one socket impossible.
	maxWSMessageBytes int64 = 32 << 10

	// wsIdleTimeout drops a socket that has sent neither a message nor a pong
	// within this window. The write loop pings every 30s, so a healthy client
	// refreshes the deadline twice per window.
	wsIdleTimeout = 60 * time.Second
)

// checkOrigin enforces the configured origin allowlist for WebSocket
// upgrades. Browsers always send Origin; native clients (Electron/Swift) may
// not, and those are gated by the AUTH token instead.
func (h *Handler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}

	if len(h.allowedOrigins) == 0 {
		// Dev default: same-host only, matching the pre-allowlist behaviour.
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}

		return u.Host == r.Host
	}

	candidate := strings.ToLower(strings.TrimSuffix(origin, "/"))
	for _, allowed := range h.allowedOrigins {
		if candidate == allowed {
			return true
		}
	}

	log.Warn().Str("origin", origin).Msg("Rejected websocket upgrade from disallowed origin")

	return false
}

func (h *Handler) upgrader() websocket.Upgrader {
	return websocket.Upgrader{CheckOrigin: h.checkOrigin}
}
```

Add `"strings"` to the imports of `internal/api/ws.go`.

Then change the upgrade call (line 62) from `upgrader.Upgrade(w, r, nil)` to:

```go
	up := h.upgrader()

	conn, err := up.Upgrade(w, r, nil)
```

- [ ] **Step 5: Apply the frame cap and idle deadline**

In `internal/api/ws.go`, immediately after the `defer func() { _ = conn.Close() }()` line, add:

```go
	conn.SetReadLimit(maxWSMessageBytes)
```

Replace the post-auth deadline reset (line 115, `_ = conn.SetReadDeadline(time.Time{})`) with:

```go
	// 2. Swap the auth deadline for a rolling idle deadline. A pong or any
	// inbound message refreshes it; a silent socket is reaped after
	// wsIdleTimeout instead of pinning a goroutine forever.
	_ = conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
	})
```

In the read loop, refresh the deadline after each successful read — insert immediately after the `if err != nil { ... break }` block that follows `conn.ReadMessage()`:

```go
		_ = conn.SetReadDeadline(time.Now().Add(wsIdleTimeout))
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/api/ -run TestWSHandler -v`
Expected: PASS — the four new tests plus the pre-existing `TestWSHandler_*` tests.

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 7: Wire the allowlist in main**

In `cmd/server/main.go`, before the `handler := api.NewHandler(...)` line, add:

```go
	// Comma-separated, e.g. "https://themighty.gg,https://www.themighty.gg".
	// Empty in local dev, where the same-host fallback applies.
	var allowedOrigins []string
	if raw := os.Getenv("ALLOWED_ORIGINS"); raw != "" {
		allowedOrigins = strings.Split(raw, ",")
	}
```

Add `"strings"` to the imports of `cmd/server/main.go`, and extend the constructor:

```go
	handler := api.NewHandler(svc, authSvc,
		api.WithRateLimiter(limiter),
		api.WithAllowedOrigins(allowedOrigins))
```

- [ ] **Step 8: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/api cmd/server/main.go
git commit -m "feat(api): websocket origin allowlist, frame cap, idle timeout"
```

---

### Task 5: WebSocket message rate cap

Per-connection, in-process — a socket that floods costs only the goroutine already serving it, so there is no reason to pay a Redis round trip per frame.

**Files:**
- Create: `internal/ratelimit/bucket.go`
- Create: `internal/ratelimit/bucket_test.go`
- Modify: `internal/api/options.go`
- Modify: `internal/api/handler.go` (Handler struct)
- Modify: `internal/api/ws.go` (read loop)
- Modify: `internal/api/ws_hardening_test.go`

**Interfaces:**
- Consumes: `api.Option` from Task 2; `maxWSMessageBytes`/`wsIdleTimeout` from Task 4.
- Produces: `ratelimit.NewBucket(capacity, refillPerSec float64, now time.Time) *Bucket`, `(*Bucket).Allow(now time.Time) bool`, and `api.WithWSMessageRate(perSec, burst float64) Option`.

- [ ] **Step 1: Write the failing bucket test**

Create `internal/ratelimit/bucket_test.go`:

```go
package ratelimit

import (
	"testing"
	"time"
)

func TestBucketAllowsBurstThenDenies(t *testing.T) {
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	b := NewBucket(3, 10, start)

	for i := range 3 {
		if !b.Allow(start) {
			t.Fatalf("call %d: expected allowed within burst", i+1)
		}
	}

	if b.Allow(start) {
		t.Fatal("expected the 4th call at t=0 to be denied")
	}
}

func TestBucketRefillsAtTheConfiguredRate(t *testing.T) {
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	b := NewBucket(2, 10, start)

	b.Allow(start)
	b.Allow(start)

	if b.Allow(start) {
		t.Fatal("expected the bucket to be empty")
	}

	// 10 tokens/sec means one token every 100ms.
	if !b.Allow(start.Add(100 * time.Millisecond)) {
		t.Fatal("expected one token to have refilled after 100ms")
	}
}

func TestBucketCapsRefillAtCapacity(t *testing.T) {
	start := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	b := NewBucket(2, 10, start)

	later := start.Add(1 * time.Hour)

	if !b.Allow(later) || !b.Allow(later) {
		t.Fatal("expected a full bucket after idling")
	}

	if b.Allow(later) {
		t.Fatal("expected refill to be capped at capacity")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/ratelimit/ -run TestBucket -v`
Expected: FAIL — `undefined: NewBucket`.

- [ ] **Step 3: Implement the in-process bucket**

Create `internal/ratelimit/bucket.go`:

```go
package ratelimit

import "time"

// Bucket is an in-process token bucket for limits scoped to a single
// goroutine — currently the per-connection WebSocket message rate. It is NOT
// safe for concurrent use; callers must own it exclusively. For limits that
// must hold across connections or restarts, use Limiter instead.
type Bucket struct {
	capacity float64
	refill   float64
	tokens   float64
	last     time.Time
}

// NewBucket returns a full bucket holding capacity tokens, refilling at
// refillPerSec tokens per second, starting at now.
func NewBucket(capacity, refillPerSec float64, now time.Time) *Bucket {
	return &Bucket{
		capacity: capacity,
		refill:   refillPerSec,
		tokens:   capacity,
		last:     now,
	}
}

// Allow consumes one token, reporting whether one was available at now.
func (b *Bucket) Allow(now time.Time) bool {
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * b.refill
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}

		b.last = now
	}

	if b.tokens < 1 {
		return false
	}

	b.tokens--

	return true
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/ratelimit/ -run TestBucket -v`
Expected: PASS — 3 tests.

- [ ] **Step 5: Write the failing WebSocket test**

Append to `internal/api/ws_hardening_test.go` (and add `"time"` to its imports):

```go
func TestWSHandlerClosesConnectionOnMessageFlood(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithWSMessageRate(2, 2)(handler)

	server := serveWS(t, handler)
	conn := dialWS(t, server, "/games/game-1/ws", generateValidToken("user-1", "alice"))

	// Burst 2 means the third immediate move is over budget.
	for range 5 {
		_ = conn.WriteJSON(map[string]any{
			keyType:          WSMessageTypeMove,
			keyMoveType:      "pass",
			"payload":        nil,
			"client_version": 1,
		})
	}

	_ = conn.Conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var closeErr *websocket.CloseError

	for {
		_, _, err := conn.Conn.ReadMessage()
		if err != nil {
			if !errors.As(err, &closeErr) {
				t.Fatalf("expected a websocket close error, got %v", err)
			}

			break
		}
	}

	if closeErr.Code != websocket.ClosePolicyViolation {
		t.Fatalf("expected close code %d, got %d", websocket.ClosePolicyViolation, closeErr.Code)
	}
}
```

Add `"errors"` to the imports of `internal/api/ws_hardening_test.go`.

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./internal/api/ -run TestWSHandlerClosesConnectionOnMessageFlood -v`
Expected: FAIL — `undefined: WithWSMessageRate`.

- [ ] **Step 7: Add the option and enforce it in the read loop**

Append to `internal/api/options.go`:

```go
// WithWSMessageRate caps inbound WebSocket messages per connection at perSec
// messages/second with a burst of burst (spec Section 3: 10/sec, burst 20).
// Zero or negative values disable the cap.
func WithWSMessageRate(perSec, burst float64) Option {
	return func(h *Handler) {
		h.wsMessagesPerSec = perSec
		h.wsMessageBurst = burst
	}
}
```

Extend the `Handler` struct in `internal/api/handler.go`:

```go
type Handler struct {
	svc              GameService
	authSvc          TokenValidator
	limiter          *ratelimit.Limiter
	allowedOrigins   []string
	wsMessagesPerSec float64
	wsMessageBurst   float64
}
```

In `internal/api/ws.go`, add a helper below `checkOrigin`:

```go
// closeWithCode tells the client exactly why the socket is going away before
// hanging up, so a buggy client can back off rather than reconnect-loop into
// the same wall. The write mutex is required because the write loop may be
// mid-frame on the same connection.
func closeWithCode(conn *websocket.Conn, code int, reason string, writeMu *sync.Mutex) {
	writeMu.Lock()
	defer writeMu.Unlock()

	msg := websocket.FormatCloseMessage(code, reason)
	_ = conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
}
```

Immediately before the read loop (`for {` following the write goroutine), add:

```go
	var msgBucket *ratelimit.Bucket
	if h.wsMessagesPerSec > 0 && h.wsMessageBurst > 0 {
		msgBucket = ratelimit.NewBucket(h.wsMessageBurst, h.wsMessagesPerSec, time.Now())
	}
```

Inside the read loop, immediately after the deadline refresh added in Task 4, add:

```go
		if msgBucket != nil && !msgBucket.Allow(time.Now()) {
			log.Warn().
				Str("game_id", gameID).
				Str("user_id", claims.UserID).
				Msg("WebSocket message rate exceeded, closing socket")
			closeWithCode(conn, websocket.ClosePolicyViolation, "rate limit exceeded", &wsWriteMu)

			break
		}
```

Add `"github.com/joekhosbayar/go-mighty/internal/ratelimit"` to the imports of `internal/api/ws.go`.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/api/ -run TestWSHandler -v`
Expected: PASS.

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 9: Wire the production values in main**

In `cmd/server/main.go`, extend the constructor:

```go
	handler := api.NewHandler(svc, authSvc,
		api.WithRateLimiter(limiter),
		api.WithAllowedOrigins(allowedOrigins),
		api.WithWSMessageRate(10, 20))
```

- [ ] **Step 10: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/ratelimit internal/api cmd/server/main.go
git commit -m "feat(api): cap websocket message rate per connection"
```

---

### Task 6: Concurrent connection caps per user and per IP

Behind Caddy every connection appears to come from the proxy's container IP, so a per-IP cap is meaningless without a trusted-proxy-aware client IP. Both land together.

**Files:**
- Create: `internal/api/clientip.go`
- Create: `internal/api/clientip_test.go`
- Create: `internal/api/connlimit.go`
- Create: `internal/api/connlimit_test.go`
- Modify: `internal/api/options.go`
- Modify: `internal/api/handler.go` (Handler struct + `NewHandler`)
- Modify: `internal/api/ws.go` (post-auth acquire/release)
- Modify: `internal/api/ws_hardening_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes: `api.Option` from Task 2.
- Produces: `api.ClientIP(r *http.Request, trustProxy bool) string`, `api.WithTrustedProxy(trust bool) Option`, `api.WithConnLimits(perUser, perIP int) Option`, and the unexported `newConnRegistry(maxPerUser, maxPerIP int) *connRegistry` with `(*connRegistry).acquire(userID, ip string) (release func(), err error)` returning `errTooManyUserConns` / `errTooManyIPConns`.

- [ ] **Step 1: Write the failing client-IP test**

Create `internal/api/clientip_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPUsesRemoteAddrWhenProxyIsUntrusted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games", nil)
	req.RemoteAddr = "10.0.0.5:41234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ClientIP(req, false); got != "10.0.0.5" {
		t.Fatalf("expected 10.0.0.5, got %q", got)
	}
}

func TestClientIPUsesForwardedForWhenProxyIsTrusted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games", nil)
	req.RemoteAddr = "172.18.0.2:41234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 172.18.0.2")

	if got := ClientIP(req, true); got != "1.2.3.4" {
		t.Fatalf("expected 1.2.3.4, got %q", got)
	}
}

func TestClientIPFallsBackWhenForwardedForIsAbsent(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games", nil)
	req.RemoteAddr = "172.18.0.2:41234"

	if got := ClientIP(req, true); got != "172.18.0.2" {
		t.Fatalf("expected 172.18.0.2, got %q", got)
	}
}
```

- [ ] **Step 2: Write the failing registry test**

Create `internal/api/connlimit_test.go`:

```go
package api

import (
	"errors"
	"testing"
)

func TestConnRegistryEnforcesPerUserCap(t *testing.T) {
	t.Parallel()

	reg := newConnRegistry(2, 100)

	r1, err := reg.acquire("user-1", "1.2.3.4")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := reg.acquire("user-1", "1.2.3.5"); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	if _, err := reg.acquire("user-1", "1.2.3.6"); !errors.Is(err, errTooManyUserConns) {
		t.Fatalf("expected errTooManyUserConns, got %v", err)
	}

	// Releasing frees a slot.
	r1()

	if _, err := reg.acquire("user-1", "1.2.3.7"); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestConnRegistryEnforcesPerIPCap(t *testing.T) {
	t.Parallel()

	reg := newConnRegistry(100, 2)

	if _, err := reg.acquire("user-1", "1.2.3.4"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := reg.acquire("user-2", "1.2.3.4"); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	if _, err := reg.acquire("user-3", "1.2.3.4"); !errors.Is(err, errTooManyIPConns) {
		t.Fatalf("expected errTooManyIPConns, got %v", err)
	}
}

func TestConnRegistryDoesNotLeakUserSlotWhenIPCapRejects(t *testing.T) {
	t.Parallel()

	reg := newConnRegistry(1, 1)

	release, err := reg.acquire("user-1", "1.2.3.4")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// user-2 is rejected on the IP cap; that must not consume user-2's own
	// per-user budget.
	if _, err := reg.acquire("user-2", "1.2.3.4"); !errors.Is(err, errTooManyIPConns) {
		t.Fatalf("expected errTooManyIPConns, got %v", err)
	}

	release()

	if _, err := reg.acquire("user-2", "1.2.3.4"); err != nil {
		t.Fatalf("user-2 should have a free slot: %v", err)
	}
}

func TestConnRegistryDisabledWhenCapsAreZero(t *testing.T) {
	t.Parallel()

	reg := newConnRegistry(0, 0)

	for range 50 {
		if _, err := reg.acquire("user-1", "1.2.3.4"); err != nil {
			t.Fatalf("caps of zero must disable limiting, got %v", err)
		}
	}
}
```

- [ ] **Step 3: Run both to verify they fail**

Run: `go test ./internal/api/ -run 'TestClientIP|TestConnRegistry' -v`
Expected: FAIL — `undefined: ClientIP`, `undefined: newConnRegistry`.

- [ ] **Step 4: Implement the client-IP helper**

Create `internal/api/clientip.go`:

```go
package api

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the caller's IP address.
//
// Behind the Caddy container every request's RemoteAddr is the proxy's
// address, which would collapse all clients into one bucket, so when
// trustProxy is set the first X-Forwarded-For entry wins. That header is
// trivially forged by a direct caller, which is why trusting it is opt-in and
// only enabled in the deployment where the security group makes Caddy the
// only possible source of traffic.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
```

- [ ] **Step 5: Implement the connection registry**

Create `internal/api/connlimit.go`:

```go
package api

import (
	"errors"
	"sync"
)

var (
	errTooManyUserConns = errors.New("too many concurrent connections for this user")
	errTooManyIPConns   = errors.New("too many concurrent connections from this address")
)

// connRegistry counts live WebSocket connections per user and per source IP
// (spec Section 3, Layer 2: ~3/user, ~20/IP). A cap of zero disables that
// dimension.
type connRegistry struct {
	mu         sync.Mutex
	perUser    map[string]int
	perIP      map[string]int
	maxPerUser int
	maxPerIP   int
}

func newConnRegistry(maxPerUser, maxPerIP int) *connRegistry {
	return &connRegistry{
		perUser:    make(map[string]int),
		perIP:      make(map[string]int),
		maxPerUser: maxPerUser,
		maxPerIP:   maxPerIP,
	}
}

// acquire reserves a slot for one connection. The returned release must be
// called exactly once when the connection ends; it is idempotent-safe only in
// the sense that callers should defer it immediately after a nil-error
// acquire.
func (c *connRegistry) acquire(userID, ip string) (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxPerUser > 0 && c.perUser[userID] >= c.maxPerUser {
		return nil, errTooManyUserConns
	}

	if c.maxPerIP > 0 && c.perIP[ip] >= c.maxPerIP {
		return nil, errTooManyIPConns
	}

	c.perUser[userID]++
	c.perIP[ip]++

	var once sync.Once

	return func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()

			// Delete at zero so the maps don't grow without bound across the
			// lifetime of the process.
			if c.perUser[userID]--; c.perUser[userID] <= 0 {
				delete(c.perUser, userID)
			}

			if c.perIP[ip]--; c.perIP[ip] <= 0 {
				delete(c.perIP, ip)
			}
		})
	}, nil
}
```

- [ ] **Step 6: Run both to verify they pass**

Run: `go test ./internal/api/ -run 'TestClientIP|TestConnRegistry' -v`
Expected: PASS — 7 tests.

- [ ] **Step 7: Write the failing WebSocket cap test**

Append to `internal/api/ws_hardening_test.go`:

```go
func TestWSHandlerRejectsExcessConnectionsForOneUser(t *testing.T) {
	t.Parallel()

	handler, cleanup := setupWSTestHandler(t)
	t.Cleanup(cleanup)
	WithConnLimits(1, 100)(handler)

	server := serveWS(t, handler)
	token := generateValidToken("user-1", "alice")

	first := dialWS(t, server, "/games/game-1/ws", token)

	// Keep the first socket alive; the second must be refused.
	second := dialWS(t, server, "/games/game-1/ws", token)

	_ = second.Conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	var closeErr *websocket.CloseError

	for {
		_, _, err := second.Conn.ReadMessage()
		if err != nil {
			if !errors.As(err, &closeErr) {
				t.Fatalf("expected a websocket close error, got %v", err)
			}

			break
		}
	}

	if closeErr.Code != websocket.CloseTryAgainLater {
		t.Fatalf("expected close code %d, got %d", websocket.CloseTryAgainLater, closeErr.Code)
	}

	_ = first.Conn.Close()
}
```

- [ ] **Step 8: Run it to verify it fails**

Run: `go test ./internal/api/ -run TestWSHandlerRejectsExcessConnections -v`
Expected: FAIL — `undefined: WithConnLimits`.

- [ ] **Step 9: Add the options and enforce them in the handler**

Append to `internal/api/options.go`:

```go
// WithConnLimits caps concurrent WebSocket connections per user and per
// source IP (spec Section 3: ~3/user, ~20/IP). Zero disables a dimension.
func WithConnLimits(perUser, perIP int) Option {
	return func(h *Handler) { h.conns = newConnRegistry(perUser, perIP) }
}

// WithTrustedProxy makes the handler read the client address from
// X-Forwarded-For. Enable it only where an ingress proxy is the sole possible
// source of traffic — otherwise callers can forge their own IP and sidestep
// per-IP caps.
func WithTrustedProxy(trust bool) Option {
	return func(h *Handler) { h.trustProxy = trust }
}
```

Extend the `Handler` struct in `internal/api/handler.go`:

```go
type Handler struct {
	svc              GameService
	authSvc          TokenValidator
	limiter          *ratelimit.Limiter
	allowedOrigins   []string
	wsMessagesPerSec float64
	wsMessageBurst   float64
	conns            *connRegistry
	trustProxy       bool
}
```

In `internal/api/ws.go`, reuse the `closeWithCode` helper added in Task 5 —
`CloseTryAgainLater` (1013) signals a capacity limit rather than misbehaviour,
so a well-behaved client knows to retry instead of treating it as fatal.

Immediately after the successful `ValidateToken` block and before the deadline reset added in Task 4, insert:

```go
	if h.conns != nil {
		release, connErr := h.conns.acquire(claims.UserID, ClientIP(r, h.trustProxy))
		if connErr != nil {
			log.Warn().
				Str("game_id", gameID).
				Str("user_id", claims.UserID).
				Err(connErr).
				Msg("Rejected websocket: connection cap reached")
			closeWithCode(conn, websocket.CloseTryAgainLater, connErr.Error(), &wsWriteMu)

			return
		}

		defer release()
	}
```

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: PASS — every api test, new and pre-existing, with no goleak failures.

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 11: Wire the production values in main**

In `cmd/server/main.go`, add before the constructor:

```go
	// Only true where an ingress proxy is the sole source of traffic — see
	// WithTrustedProxy. Set in the prod compose .env, absent locally.
	trustProxy := os.Getenv("TRUST_PROXY_HEADERS") == "true"
```

and extend the constructor to its final form:

```go
	handler := api.NewHandler(svc, authSvc,
		api.WithRateLimiter(limiter),
		api.WithAllowedOrigins(allowedOrigins),
		api.WithWSMessageRate(10, 20),
		api.WithConnLimits(3, 20),
		api.WithTrustedProxy(trustProxy))
```

- [ ] **Step 12: Verify the whole stack still runs**

Run: `go build ./... && docker compose up -d --build`
Then: `curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/games`
Expected: `200`
Then: `docker compose logs mighty | tail -20` — expect no panics or repeated errors.
Run: `docker compose down`

- [ ] **Step 13: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/api cmd/server/main.go
git commit -m "feat(api): cap concurrent websockets per user and per IP"
```

---

### Task 7: Custom Caddy image with the rate-limit plugin

The stock `caddy:2-alpine` image has no rate limiting. `xcaddy` compiles `mholt/caddy-ratelimit` into a Caddy binary; the result gets its own ECR repository so the instance pulls it the same way it pulls the app.

**Files:**
- Create: `deploy/compose/Dockerfile.caddy`
- Create: `deploy/scripts/build-caddy.sh`
- Modify: `deploy/terraform/ecr.tf`

**Interfaces:**
- Consumes: the existing ECR/OpenTofu setup from Plan 1.
- Produces: an ECR repository named `mighty-caddy` and an image tag `<account-id>.dkr.ecr.us-east-1.amazonaws.com/mighty-caddy:latest` (linux/arm64) whose `caddy list-modules` includes `http.handlers.rate_limit`. Task 8 consumes that image.

- [ ] **Step 1: Write the Dockerfile**

Create `deploy/compose/Dockerfile.caddy`:

```dockerfile
# Caddy with the rate-limit plugin compiled in. The stock image has no
# rate limiting, and xcaddy is the only supported way to add a module.
# Versions are pinned so a rebuild can't silently change the edge.
FROM caddy:2.10-builder-alpine AS builder

RUN xcaddy build v2.10.0 \
    --with github.com/mholt/caddy-ratelimit@v0.1.0

FROM caddy:2.10-alpine

COPY --from=builder /usr/bin/caddy /usr/bin/caddy
```

- [ ] **Step 2: Add the ECR repository**

Append to `deploy/terraform/ecr.tf`:

```hcl
resource "aws_ecr_repository" "caddy" {
  name = "mighty-caddy"

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "caddy" {
  repository = aws_ecr_repository.caddy.name

  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "keep last 5 images"
      selection = {
        tagStatus   = "any"
        countType   = "imageCountMoreThan"
        countNumber = 5
      }
      action = { type = "expire" }
    }]
  })
}
```

Append to `deploy/terraform/outputs.tf`:

```hcl
output "caddy_ecr_repo_url" {
  value = aws_ecr_repository.caddy.repository_url
}
```

- [ ] **Step 3: Apply the Terraform change**

Run:

```bash
tofu -chdir=deploy/terraform init
tofu -chdir=deploy/terraform apply
```

Expected: a plan adding `aws_ecr_repository.caddy` and `aws_ecr_lifecycle_policy.caddy` (2 to add, 0 to change, 0 to destroy). Approve it.

Then: `tofu -chdir=deploy/terraform output -raw caddy_ecr_repo_url`
Expected: `<account-id>.dkr.ecr.us-east-1.amazonaws.com/mighty-caddy`

- [ ] **Step 4: Write the build script**

Create `deploy/scripts/build-caddy.sh`:

```bash
#!/usr/bin/env bash
# Builds the custom Caddy (rate-limit plugin) for the Graviton instance and
# pushes it to ECR. Run this whenever Dockerfile.caddy changes — the image is
# not rebuilt by the app deploy.
set -euo pipefail
cd "$(dirname "$0")/../.."

REGION=us-east-1
REPO=$(tofu -chdir=deploy/terraform output -raw caddy_ecr_repo_url)
ECR_HOST=${REPO%%/*}

aws ecr get-login-password --region "$REGION" \
	| docker login --username AWS --password-stdin "$ECR_HOST"

docker buildx build \
	--platform linux/arm64 \
	-f deploy/compose/Dockerfile.caddy \
	-t "${REPO}:latest" \
	--push \
	deploy/compose

echo "Pushed ${REPO}:latest"
```

Run: `chmod +x deploy/scripts/build-caddy.sh`

- [ ] **Step 5: Build, push, and verify the plugin is present**

Run: `./deploy/scripts/build-caddy.sh`
Expected: ends with `Pushed <account-id>.dkr.ecr.us-east-1.amazonaws.com/mighty-caddy:latest`.

Verify the module actually compiled in (runs the arm64 image via emulation on an x86 host; native on Apple Silicon):

```bash
REPO=$(tofu -chdir=deploy/terraform output -raw caddy_ecr_repo_url)
docker run --rm --platform linux/arm64 "${REPO}:latest" caddy list-modules | grep rate_limit
```

Expected: `http.handlers.rate_limit`

- [ ] **Step 6: Commit**

```bash
git add deploy/compose/Dockerfile.caddy deploy/scripts/build-caddy.sh deploy/terraform/ecr.tf deploy/terraform/outputs.tf
git commit -m "feat(deploy): build Caddy with the rate-limit plugin"
```

---

### Task 8: Edge hardening — rate-limit zones, CORS, security headers

The last task turns on Layer 1 and gives the Go layer the environment it needs (`ALLOWED_ORIGINS`, `TRUST_PROXY_HEADERS`), then verifies the whole thing against the live endpoint.

**Files:**
- Modify: `deploy/compose/Caddyfile`
- Modify: `deploy/compose/docker-compose.prod.yml`
- Modify: `deploy/compose/remote-deploy.sh`

**Interfaces:**
- Consumes: the `mighty-caddy` image from Task 7; the `ALLOWED_ORIGINS` and `TRUST_PROXY_HEADERS` env vars read by `cmd/server/main.go` in Tasks 4 and 6.
- Produces: the deployed edge. Nothing later in this plan depends on it.

- [ ] **Step 1: Rewrite the Caddyfile**

Replace the entire contents of `deploy/compose/Caddyfile` with:

```caddyfile
{
	email {$ACME_EMAIL}

	# The rate-limit plugin has no default position in Caddy's directive
	# order, so it must be placed explicitly or the config won't load.
	order rate_limit before basicauth

	servers {
		timeouts {
			# Only the header timeout is safe to set. read_body, write, and
			# idle all apply to the hijacked connection behind a WebSocket
			# upgrade and would drop players mid-game.
			read_header 10s
		}
	}
}

# Naming the site by hostname makes Caddy provision the certificate and
# redirect all plain HTTP to HTTPS automatically (spec Section 3, Layer 1) —
# port 80 stays open only for that redirect and the ACME challenge.
{$API_DOMAIN} {
	encode gzip

	header {
		Strict-Transport-Security "max-age=31536000; includeSubDomains"
		X-Content-Type-Options "nosniff"
		X-Frame-Options "DENY"
		Referrer-Policy "strict-origin-when-cross-origin"
		-Server
	}

	request_body {
		max_size 64KB
	}

	# Per-IP zones (spec Section 3, Layer 1). Abuse control, not DDoS
	# protection: a distributed flood stays under every per-IP limit. The
	# strict zone covers the endpoints that allocate state.
	rate_limit {
		zone strict {
			match {
				method POST
				path /games /games/*/join
			}
			key {remote_host}
			events 10
			window 1m
		}

		zone api {
			key {remote_host}
			events 100
			window 1m
		}
	}

	# CORS preflight, answered at the edge and only for allowed origins.
	# Different fields AND together; repeated Origin values OR together, so
	# this matches OPTIONS from either SPA host and nothing else.
	@preflight {
		method OPTIONS
		header Origin https://themighty.gg
		header Origin https://www.themighty.gg
	}

	handle @preflight {
		header {
			Access-Control-Allow-Origin "{http.request.header.Origin}"
			Access-Control-Allow-Methods "GET, POST, OPTIONS"
			Access-Control-Allow-Headers "Authorization, Content-Type"
			Access-Control-Allow-Credentials "true"
			Access-Control-Max-Age "600"
			Vary "Origin"
		}
		respond 204
	}

	@cors {
		header Origin https://themighty.gg
		header Origin https://www.themighty.gg
	}

	handle {
		header @cors {
			Access-Control-Allow-Origin "{http.request.header.Origin}"
			Access-Control-Allow-Credentials "true"
			Vary "Origin"
		}

		# reverse_proxy handles the WebSocket upgrade natively and appends
		# X-Forwarded-For, which the app reads for per-IP connection caps.
		reverse_proxy mighty:8080
	}
}
```

- [ ] **Step 2: Point the compose stack at the custom image**

In `deploy/compose/docker-compose.prod.yml`, replace the caddy service's image line:

```yaml
  caddy:
    image: ${CADDY_IMAGE}
```

- [ ] **Step 3: Render the new environment on the instance**

In `deploy/compose/remote-deploy.sh`, inside the `.env` heredoc, add after the `ECR_IMAGE=` line:

```bash
CADDY_IMAGE=${ECR_HOST}/mighty-caddy:latest
```

and after the `LOG_LEVEL=info` line:

```bash
ALLOWED_ORIGINS=https://themighty.gg,https://www.themighty.gg
TRUST_PROXY_HEADERS=true
```

- [ ] **Step 4: Validate the Caddyfile before shipping it**

Run:

```bash
REPO=$(tofu -chdir=deploy/terraform output -raw caddy_ecr_repo_url)
docker run --rm --platform linux/arm64 \
  -e API_DOMAIN=api.themighty.gg -e ACME_EMAIL=joekhosbayar123@gmail.com \
  -v "$PWD/deploy/compose/Caddyfile:/etc/caddy/Caddyfile:ro" \
  "${REPO}:latest" caddy validate --config /etc/caddy/Caddyfile
```

Expected: `Valid configuration`. If it reports an unrecognized directive, the `order rate_limit` line or the plugin build (Task 7) is wrong — fix before deploying.

- [ ] **Step 5: Deploy**

Run: `./deploy/scripts/deploy.sh`
Expected: the SSM invocation ends with `"Status": "Success"`.

- [ ] **Step 6: Verify the security headers and TLS**

Run:

```bash
curl -sI https://api.themighty.gg/healthz | grep -Ei 'strict-transport|x-content-type|x-frame|referrer-policy|^server'
```

Expected: `Strict-Transport-Security`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, and `Referrer-Policy` all present; no `Server: Caddy` line.

- [ ] **Step 7: Verify the general rate-limit zone**

Run:

```bash
for i in $(seq 1 130); do
  curl -s -o /dev/null -w "%{http_code}\n" https://api.themighty.gg/healthz
done | sort | uniq -c
```

Expected: roughly 100 × `200` followed by ~30 × `429`. If everything returns 200, the `rate_limit` directive is not loading — recheck Step 4.

- [ ] **Step 8: Verify the strict zone and the body cap**

Run:

```bash
for i in $(seq 1 15); do
  curl -s -o /dev/null -w "%{http_code}\n" -X POST https://api.themighty.gg/games
done | sort | uniq -c
```

Expected: about 10 × `401` (unauthenticated, but past the edge) then `429` — the strict zone bites at 10/min regardless of auth.

Wait 60 seconds for the window to reset, then:

```bash
python3 -c "print('{\"pad\":\"' + 'a'*70000 + '\"}')" > /tmp/big.json
curl -s -o /dev/null -w "%{http_code}\n" -X POST -H 'Content-Type: application/json' \
  --data-binary @/tmp/big.json https://api.themighty.gg/games
```

Expected: `413`

- [ ] **Step 9: Verify CORS**

Run:

```bash
curl -si -X OPTIONS https://api.themighty.gg/games \
  -H 'Origin: https://themighty.gg' \
  -H 'Access-Control-Request-Method: POST' | head -20
```

Expected: `HTTP/2 204` with `access-control-allow-origin: https://themighty.gg`.

Run the same with a foreign origin:

```bash
curl -si -X OPTIONS https://api.themighty.gg/games \
  -H 'Origin: https://evil.example' \
  -H 'Access-Control-Request-Method: POST' | grep -i 'access-control-allow-origin' || echo "no CORS header (correct)"
```

Expected: `no CORS header (correct)`.

- [ ] **Step 10: Verify WebSockets still work through the hardened edge**

Wait 60 seconds so the strict zone resets, then run (`npm i -g wscat` if needed):

```bash
wscat -c wss://api.themighty.gg/games/probe/ws -H 'Origin: https://themighty.gg'
```

Expected: the connection opens (`Connected`). Send nothing; after ~5 seconds the server replies with `{"type":"ERROR","error":"auth timed out"}` and closes — that is the unchanged AUTH handshake proving upgrade, framing, and close all survive the proxy.

Then confirm the origin allowlist is live at the app layer:

```bash
wscat -c wss://api.themighty.gg/games/probe/ws -H 'Origin: https://evil.example'
```

Expected: the handshake fails with `Unexpected server response: 403`.

- [ ] **Step 11: Check the app logs for the new safeguards**

Run:

```bash
INSTANCE_ID=$(tofu -chdir=deploy/terraform output -raw instance_id)
aws ssm start-session --target "$INSTANCE_ID" --region us-east-1 \
  --document-name AWS-StartInteractiveCommand \
  --parameters 'command=["docker logs --tail 50 mighty-mighty-1"]'
```

Expected: startup logs with no `Rate limiter unavailable` warnings (which would mean the limiter can't reach Redis) and a `Rejected websocket upgrade from disallowed origin` entry from Step 10's second probe.

- [ ] **Step 12: Commit**

```bash
git add deploy/compose/Caddyfile deploy/compose/docker-compose.prod.yml deploy/compose/remote-deploy.sh
git commit -m "feat(deploy): edge rate limiting, CORS allowlist, security headers"
```

---

## Post-implementation notes

**What this plan deliberately does not build** (spec Section 3, "DDoS posture"): the Cloudflare escalation runbook stays documentation. Per-IP limiting is abuse control — a distributed L7 flood stays under every per-IP limit and still costs the box a TCP accept, a TLS handshake, and request parsing before the 429. That is an accepted MVP risk; the AWS Budgets alert from Plan 1 is the tripwire, and the escalation steps (Cloudflare in front, security group narrowed to Cloudflare ranges, **Elastic IP rotated at the same time** so the old address in DNS history can't be hit directly) are built only if attacked.

**Already shipped in Plan 1** (Layer 3 of the spec, no work here): security group 443/80 only, no SSH, SSM Session Manager access, Postgres/Redis with no host ports, secrets from SSM Parameter Store rendered to `.env` at deploy, `dnf-automatic` security updates. Layers 0 and 0.5 (Shield Standard, Cognito auth throttling) are automatic.

**Next:** Plan 5 — OpenTelemetry Collector + Grafana Cloud, Grafana alerting, and GitHub Actions → ECR → SSM CI/CD. The rate-limit rejections logged here (`Per-user rate limit exceeded`, `WebSocket message rate exceeded`, `Rejected websocket: connection cap reached`) are the natural first alerting signals once that pipeline exists.
