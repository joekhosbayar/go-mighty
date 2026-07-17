# Correctness & Concurrency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the eight adversarial-review findings F1–F8: make move processing actually mutually exclusive (token lock + CAS save), and make the friend mechanic, joker leads, and all-pass bidding behave per the rules.

**Architecture:** Evolutionary — same Redis-lock + optimistic-version design, done correctly. The `RedisStore` interface changes shape (`AcquireLock` returns a token, `SaveGame` becomes CAS, `CheckVersion` is deleted); the game engine (`internal/game`) gains partner reveal, `no_friend`, `called_suit`, and redeal. Wire contract may break freely; the web frontend adapts afterward.

**Tech Stack:** Go 1.24+, go-redis v9 (`redis.NewScript` Lua), godog Gherkin E2E, docker compose (Redis on `localhost:6379`, backend on `:8080`).

**Spec:** `docs/superpowers/specs/2026-07-17-correctness-concurrency-design.md`

## Global Constraints

- Working directory: `/Users/joekhosbayar/mighty/go-mighty`.
- Lock: `SET game:{id}:lock <token> NX PX 5000`, token = 16 random bytes hex; retry backoff 50ms/100ms/200ms (3 retries); failure → `ErrLockFailed` → service `ErrGameBusy` ("game busy") → HTTP 409.
- CAS save: one Lua script; missing version key only matches `expectedVersion == 0`; both keys keep 24h TTL.
- Client-facing version semantics unchanged: `client_version` must equal current version, else stale-version rejection (`ErrStaleVersion`, text "stale version", HTTP mapping stays the generic 400).
- New payload shapes: `call_partner` → `{"card": {...}}` XOR `{"no_friend": true}` (bare `Card` still accepted); `play_card` gains `"called_suit"` (required when leading the Joker, forbidden otherwise).
- Lint gate: the repo uses golangci-lint (`.golangci.yml`); run `go vet ./...` minimum after each task.
- Unit tests: `go test ./internal/...`. Integration tests: `go test -tags=integration ./internal/store/redis/...` (needs `docker compose up -d redis`). E2E: `go test -v -tags=integration ./tests/e2e/...` (needs full stack).
- Commit after every green cycle; conventional messages (`fix:`, `feat:`, `test:`, `docs:`).

## File Structure

```
internal/store/redis/redis.go              # token lock, Lua release, CAS SaveGame, drop CheckVersion
internal/store/redis/redis_integration_test.go   # NEW: real-Redis lock/CAS/concurrency tests (tag: integration)
internal/service/game_service.go           # honor lock token, version compare, ErrGameBusy
internal/service/game_service_test.go      # fake store update + busy/stale tests
internal/api/handler.go                    # ConvertPayload call_partner; 409 mapping
internal/api/handler_test.go               # ConvertPayload call_partner tests (existing file, append)
internal/game/game.go                      # CallPartnerMove type; PlayCardMove.CalledSuit; Scores comment
internal/game/rules.go                     # reveal, no_friend, called_suit, dead-branch removal, redeal, friendScore
internal/game/rules_friend_test.go         # NEW: partner reveal / no-friend / scoring tests
internal/game/rules_joker_test.go          # NEW: joker lead tests
internal/game/rules_redeal_test.go         # NEW: all-pass redeal tests
tests/e2e/features/friend.feature          # NEW: reveal + no-friend scenarios
tests/e2e/e2e_test.go                      # new steps, joker-aware findLegalCard, record called card
docs/API_DOCUMENTATION.md                  # payload shapes, scores semantics, 409
```

---

### Task 1: Redis lock with ownership token

**Files:**
- Modify: `internal/store/redis/redis.go` (AcquireLock, ReleaseLock)
- Create: `internal/store/redis/redis_integration_test.go`

**Interfaces:**
- Consumes: existing `Store` struct with private `client *redis.Client`; `ErrLockFailed` already declared in this file.
- Produces: `AcquireLock(ctx context.Context, gameID string) (string, error)` — returns `("", ErrLockFailed)` when contended after retries; `ReleaseLock(ctx context.Context, gameID, token string) error` — Lua compare-and-delete. Task 3 (service) consumes exactly these signatures.

**Prerequisite:** `docker compose up -d redis` (Redis exposed on `localhost:6379`).

- [ ] **Step 1: Write the failing integration tests**

Create `internal/store/redis/redis_integration_test.go`:

```go
//go:build integration

package redis

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	s := NewStore(addr)
	t.Cleanup(func() { _ = s.client.Close() })

	if err := s.client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis not reachable at %s: %v", addr, err)
	}

	return s
}

func TestAcquireLockReturnsTokenAndBlocksSecondAcquirer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	gameID := "lock-test-" + t.Name()

	token, err := s.AcquireLock(ctx, gameID)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if token == "" {
		t.Fatal("expected non-empty token")
	}

	start := time.Now()

	token2, err := s.AcquireLock(ctx, gameID)
	if !errors.Is(err, ErrLockFailed) {
		t.Fatalf("expected ErrLockFailed, got token=%q err=%v", token2, err)
	}

	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("expected backoff retries (>=350ms), returned after %v", elapsed)
	}

	if err := s.ReleaseLock(ctx, gameID, token); err != nil {
		t.Fatalf("release: %v", err)
	}

	token3, err := s.AcquireLock(ctx, gameID)
	if err != nil || token3 == "" {
		t.Fatalf("expected re-acquire after release, got token=%q err=%v", token3, err)
	}

	_ = s.ReleaseLock(ctx, gameID, token3)
}

func TestReleaseLockRequiresMatchingToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	gameID := "lock-token-test-" + t.Name()

	token, err := s.AcquireLock(ctx, gameID)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Wrong token must not release the lock.
	if err := s.ReleaseLock(ctx, gameID, "bogus-token"); err != nil {
		t.Fatalf("release with wrong token should not error, got %v", err)
	}

	if _, err := s.AcquireLock(ctx, gameID); !errors.Is(err, ErrLockFailed) {
		t.Fatalf("lock should still be held after wrong-token release, got %v", err)
	}

	if err := s.ReleaseLock(ctx, gameID, token); err != nil {
		t.Fatalf("release with right token: %v", err)
	}

	token2, err := s.AcquireLock(ctx, gameID)
	if err != nil || token2 == "" {
		t.Fatalf("expected acquire after correct release, got %v", err)
	}

	_ = s.ReleaseLock(ctx, gameID, token2)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=integration -run 'TestAcquireLock|TestReleaseLock' ./internal/store/redis/ -v`
Expected: COMPILE FAILURE — `AcquireLock` returns `(bool, error)` not `(string, error)`; `ReleaseLock` takes 2 args not 3.

- [ ] **Step 3: Implement token lock**

In `internal/store/redis/redis.go`, add imports `"crypto/rand"` and `"encoding/hex"`, then replace `AcquireLock` and `ReleaseLock`:

```go
// lockBackoff is the retry schedule when the lock is contended.
var lockBackoff = []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}

// AcquireLock acquires a distributed lock for the game with a 5-second expiration.
// It returns an ownership token required to release the lock, retrying with
// backoff while contended. Returns ErrLockFailed if the lock stays held.
func (s *Store) AcquireLock(ctx context.Context, gameID string) (token string, err error) {
	start := time.Now()

	key := s.Key(gameID) + ":lock"
	defer func() {
		log.Debug().
			Str("component", "redis").
			Str("op", "AcquireLock").
			Str("key", key).
			Bool("locked", token != "").
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("AcquireLock")
	}()

	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}

	candidate := hex.EncodeToString(raw)

	for attempt := 0; ; attempt++ {
		ok, err := s.client.SetNX(ctx, key, candidate, 5*time.Second).Result()
		if err != nil {
			return "", err
		}

		if ok {
			return candidate, nil
		}

		if attempt >= len(lockBackoff) {
			return "", ErrLockFailed
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(lockBackoff[attempt]):
		}
	}
}

// releaseScript deletes the lock only when the caller still owns it.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0`)

// ReleaseLock releases the distributed lock if token matches the current owner.
func (s *Store) ReleaseLock(ctx context.Context, gameID, token string) (err error) {
	start := time.Now()

	key := s.Key(gameID) + ":lock"
	defer func() {
		log.Debug().
			Str("component", "redis").
			Str("op", "ReleaseLock").
			Str("key", key).
			Err(err).
			Dur("latency", time.Since(start)).
			Msg("ReleaseLock")
	}()

	return releaseScript.Run(ctx, s.client, []string{key}, token).Err()
}
```

Note: `internal/service` and its tests will not compile until Task 3 — that is expected; run only the store package until then.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -tags=integration -run 'TestAcquireLock|TestReleaseLock' ./internal/store/redis/ -v`
Expected: 2 PASS. (`go build ./internal/store/redis/` also clean.)

- [ ] **Step 5: Commit**

```bash
git add internal/store/redis/
git commit -m "fix: lock ownership token with retry backoff and compare-delete release"
```

---

### Task 2: CAS SaveGame, delete CheckVersion

**Files:**
- Modify: `internal/store/redis/redis.go` (SaveGame; delete CheckVersion)
- Test: `internal/store/redis/redis_integration_test.go` (append)

**Interfaces:**
- Consumes: Task 1's store.
- Produces: `SaveGame(ctx context.Context, g *game.Game, expectedVersion int64) error` — returns `ErrStaleVersion` on version mismatch; `CheckVersion` no longer exists. Task 3 consumes this.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/redis/redis_integration_test.go` (add `"sync"` and `"github.com/joekhosbayar/go-mighty/internal/game"` to imports):

```go
func TestSaveGameCAS(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := game.New("cas-test-" + t.Name())

	// Fresh game: no version key yet, expectedVersion 0 must succeed.
	if err := s.SaveGame(ctx, g, 0); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	// Wrong expected version must fail and leave state untouched.
	g2 := *g
	g2.Version = 99

	if err := s.SaveGame(ctx, &g2, 42); !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("expected ErrStaleVersion, got %v", err)
	}

	loaded, err := s.LoadGame(ctx, g.ID)
	if err != nil || loaded == nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.Version != g.Version {
		t.Fatalf("state changed on failed CAS: got v%d want v%d", loaded.Version, g.Version)
	}

	// Correct expected version must succeed.
	g.Version++
	if err := s.SaveGame(ctx, g, g.Version-1); err != nil {
		t.Fatalf("CAS save: %v", err)
	}
}

func TestConcurrentLockedWritersLoseNoUpdates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := game.New("concurrency-test-" + t.Name())

	if err := s.SaveGame(ctx, g, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const workers = 5

	const opsPerWorker = 10

	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range opsPerWorker {
				var token string
				// Spin until the lock is ours (contention is the point here).
				for {
					tok, err := s.AcquireLock(ctx, g.ID)
					if err == nil && tok != "" {
						token = tok
						break
					}

					if err != nil && !errors.Is(err, ErrLockFailed) {
						t.Errorf("acquire: %v", err)
						return
					}
				}

				cur, err := s.LoadGame(ctx, g.ID)
				if err != nil || cur == nil {
					t.Errorf("load: %v", err)
					return
				}

				loadedVersion := cur.Version
				cur.Version++

				if err := s.SaveGame(ctx, cur, loadedVersion); err != nil {
					t.Errorf("locked CAS save must not conflict: %v", err)
				}

				if err := s.ReleaseLock(ctx, g.ID, token); err != nil {
					t.Errorf("release: %v", err)
				}
			}
		}()
	}

	wg.Wait()

	final, err := s.LoadGame(ctx, g.ID)
	if err != nil || final == nil {
		t.Fatalf("final load: %v", err)
	}

	if want := g.Version + workers*opsPerWorker; final.Version != want {
		t.Fatalf("lost updates: final version %d, want %d", final.Version, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -tags=integration -run 'TestSaveGameCAS|TestConcurrentLocked' ./internal/store/redis/ -v`
Expected: COMPILE FAILURE — `SaveGame` takes 2 args, not 3.

- [ ] **Step 3: Implement CAS save and delete CheckVersion**

In `internal/store/redis/redis.go`, replace `SaveGame` and **delete the entire `CheckVersion` method**:

```go
// saveScript writes state and version only when the stored version still
// matches the caller's expectation (missing key matches expectation 0).
var saveScript = redis.NewScript(`
local cur = redis.call("GET", KEYS[2])
if (cur == false and ARGV[3] == "0") or cur == ARGV[3] then
	redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[4])
	redis.call("SET", KEYS[2], ARGV[2], "PX", ARGV[4])
	return 1
end
return 0`)

// SaveGame persists the game state via compare-and-swap on the version key.
// expectedVersion is the version loaded at the start of the operation;
// ErrStaleVersion is returned when another writer got there first.
func (s *Store) SaveGame(ctx context.Context, g *game.Game, expectedVersion int64) (err error) {
	start := time.Now()

	key := s.Key(g.ID)
	defer func() {
		event := log.Debug().
			Str("component", "redis").
			Str("op", "SaveGame").
			Str("key", key).
			Dur("latency", time.Since(start))
		if err != nil {
			event.Err(err).Msg("SaveGame failed")
		} else {
			event.
				Str("game_id", g.ID).
				Int64("version", g.Version).
				Str("status", string(g.Status)).
				Msg("SaveGame success")
		}
	}()

	data, err := json.Marshal(g)
	if err != nil {
		return err
	}

	ttlMillis := int64((24 * time.Hour) / time.Millisecond)

	res, err := saveScript.Run(ctx, s.client,
		[]string{key + ":state", key + ":version"},
		data,
		strconv.FormatInt(g.Version, 10),
		strconv.FormatInt(expectedVersion, 10),
		strconv.FormatInt(ttlMillis, 10),
	).Int()
	if err != nil {
		return err
	}

	if res == 0 {
		return ErrStaleVersion
	}

	return nil
}
```

Add `"strconv"` to imports.

- [ ] **Step 4: Run store tests**

Run: `go test -tags=integration ./internal/store/redis/ -v`
Expected: all PASS (including Task 1 tests). Other packages still don't compile — next task.

- [ ] **Step 5: Commit**

```bash
git add internal/store/redis/
git commit -m "fix: compare-and-swap SaveGame replaces standalone CheckVersion"
```

---

### Task 3: Service flow honors lock and version

**Files:**
- Modify: `internal/service/game_service.go`
- Test: `internal/service/game_service_test.go`

**Interfaces:**
- Consumes: Task 1–2 store signatures.
- Produces: `RedisStore` interface updated to the new signatures (drop `CheckVersion`); `ErrGameBusy = errors.New("game busy")` exported from `service`; `CreateGame`/`JoinGame`/`ProcessMove` use token + CAS. Task 4 (handlers) consumes `service.ErrGameBusy`.

- [ ] **Step 1: Update the fake store and write failing tests**

In `internal/service/game_service_test.go`, replace the fake store methods and add tests (keep the existing `TestJoinGame...` tests; they compile against the new fake unchanged):

```go
type fakeRedisStore struct {
	game       *game.Game
	saved      bool
	savedWith  int64
	acquireErr error
}

func (f *fakeRedisStore) SaveGame(_ context.Context, g *game.Game, expectedVersion int64) error {
	f.saved = true
	f.savedWith = expectedVersion
	f.game = g

	return nil
}

func (f *fakeRedisStore) LoadGame(_ context.Context, _ string) (*game.Game, error) {
	return f.game, nil
}

func (f *fakeRedisStore) AcquireLock(_ context.Context, _ string) (string, error) {
	if f.acquireErr != nil {
		return "", f.acquireErr
	}

	return "test-token", nil
}

func (f *fakeRedisStore) ReleaseLock(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeRedisStore) PublishEvent(_ context.Context, _ string, _ any) error {
	return nil
}

func (f *fakeRedisStore) Subscribe(_ context.Context, _ string) *redis.PubSub {
	return nil
}
```

Append tests (add imports `"errors"` and `redisstore "github.com/joekhosbayar/go-mighty/internal/store/redis"`):

```go
func TestProcessMoveReturnsBusyWhenLockContended(t *testing.T) {
	t.Parallel()

	g := game.New("game-busy")
	store := &fakeRedisStore{game: g, acquireErr: redisstore.ErrLockFailed}
	svc := &Game{redisStore: store}

	_, err := svc.ProcessMove(t.Context(), "game-busy", "p1", game.MovePass, nil, 1)
	if !errors.Is(err, ErrGameBusy) {
		t.Fatalf("expected ErrGameBusy, got %v", err)
	}

	if store.saved {
		t.Fatal("no save should happen when the lock is contended")
	}
}

func TestProcessMoveRejectsStaleClientVersion(t *testing.T) {
	t.Parallel()

	g := game.New("game-stale")
	g.Version = 5
	store := &fakeRedisStore{game: g}
	svc := &Game{redisStore: store}

	_, err := svc.ProcessMove(t.Context(), "game-stale", "p1", game.MovePass, nil, 4)
	if !errors.Is(err, redisstore.ErrStaleVersion) {
		t.Fatalf("expected ErrStaleVersion, got %v", err)
	}

	if store.saved {
		t.Fatal("no save should happen on stale version")
	}
}

func TestJoinGameSavesWithLoadedVersionAsCASExpectation(t *testing.T) {
	t.Parallel()

	g := game.New("game-cas")
	g.Version = 7
	g.Players[0] = &game.Player{ID: "p1", Name: "P1", Seat: 0, IsConnected: false}
	store := &fakeRedisStore{game: g}
	svc := &Game{redisStore: store}

	if _, err := svc.JoinGame(t.Context(), "game-cas", "p1", "P1"); err != nil {
		t.Fatalf("join: %v", err)
	}

	if store.savedWith != 7 {
		t.Fatalf("expected CAS expectation 7 (pre-bump version), got %d", store.savedWith)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/ -v`
Expected: COMPILE FAILURE — `RedisStore` interface and service call sites still use old signatures; `ErrGameBusy` undefined.

- [ ] **Step 3: Update the service**

In `internal/service/game_service.go`:

Add to the error var block:

```go
	// ErrGameBusy is returned when the game's lock cannot be acquired in time.
	ErrGameBusy = errors.New("game busy")
```

Replace the `RedisStore` interface:

```go
// RedisStore defines the interface for hot state storage of games in Redis.
type RedisStore interface {
	SaveGame(ctx context.Context, g *game.Game, expectedVersion int64) error
	LoadGame(ctx context.Context, gameID string) (*game.Game, error)
	AcquireLock(ctx context.Context, gameID string) (string, error)
	ReleaseLock(ctx context.Context, gameID, token string) error
	PublishEvent(ctx context.Context, gameID string, event any) error
	Subscribe(ctx context.Context, gameID string) *redis.PubSub
}
```

Add import `redisstore "github.com/joekhosbayar/go-mighty/internal/store/redis"`.

Add a lock helper:

```go
// withGameLock acquires the game's distributed lock, mapping contention to ErrGameBusy.
func (s *Game) withGameLock(ctx context.Context, gameID string) (release func(), err error) {
	token, err := s.redisStore.AcquireLock(ctx, gameID)
	if err != nil {
		if errors.Is(err, redisstore.ErrLockFailed) {
			return nil, ErrGameBusy
		}

		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	return func() { _ = s.redisStore.ReleaseLock(ctx, gameID, token) }, nil
}
```

In `CreateGame`, change the Redis save call to:

```go
	if err := s.redisStore.SaveGame(ctx, g, 0); err != nil {
		return nil, fmt.Errorf("failed to save game in redis: %w", err)
	}
```

In `JoinGame`, replace the lock block and both save calls:

```go
	release, err := s.withGameLock(ctx, gameID)
	if err != nil {
		return nil, err
	}
	defer release()
```

then after loading, capture `loadedVersion := g.Version` **before** any mutation (insert right after the `g == nil` check), and change both `s.redisStore.SaveGame(ctx, g)` calls to `s.redisStore.SaveGame(ctx, g, loadedVersion)`.

In `ProcessMove`, replace steps 1–3 (lock, CheckVersion, load) with:

```go
	release, err := s.withGameLock(ctx, gameID)
	if err != nil {
		return nil, err
	}
	defer release()

	g, err := s.redisStore.LoadGame(ctx, gameID)
	if err != nil {
		return nil, err
	}

	if g == nil {
		return nil, ErrGameNotFound
	}

	loadedVersion := g.Version
	if clientVersion != loadedVersion {
		return nil, redisstore.ErrStaleVersion
	}
```

and change the save call to `s.redisStore.SaveGame(ctx, g, loadedVersion)`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/ -v && go build ./... && go vet ./...`
Expected: all service tests PASS (old + new); whole repo compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/service/
git commit -m "fix: honor lock acquisition result and make version check atomic with save"
```

---

### Task 4: Handler mapping for game-busy

**Files:**
- Modify: `internal/api/handler.go` (MoveHandler, JoinGameHandler)
- Test: `internal/api/handler_test.go` (append)

**Interfaces:**
- Consumes: `service.ErrGameBusy` from Task 3.
- Produces: HTTP 409 for busy on `/games/{id}/move` and `/games/{id}/join`. (WS needs no change — `ws.go` already forwards `err.Error()` as an ERROR frame, which yields `"game busy"`.)

- [ ] **Step 1: Write the failing test**

The api test package already provides `generateValidToken(userID, username)` (`lobby_handler_test.go:77`, signs with `"testsecret"`) and builds handlers via `NewHandler(svc, service.NewAuth(&postgres.Store{}, "testsecret"))` (`ws_test.go:116`). Append to `internal/api/handler_test.go` a minimal `GameService` fake and the mapping test (add imports the file lacks: `"net/http/httptest"`, `"strings"`, `"github.com/joekhosbayar/go-mighty/internal/store/postgres"`, `goredis "github.com/redis/go-redis/v9"` — reuse existing aliases if already imported):

```go
// busyGameService fails every ProcessMove with lock contention.
type busyGameService struct{}

func (busyGameService) CreateGame(_ context.Context, _ string) (*game.Game, error) { return nil, nil }
func (busyGameService) JoinGame(_ context.Context, _, _, _ string) (*game.Game, error) {
	return nil, service.ErrGameBusy
}
func (busyGameService) ProcessMove(_ context.Context, _, _ string, _ game.MoveType, _ any, _ int64) (*game.Game, error) {
	return nil, service.ErrGameBusy
}
func (busyGameService) Subscribe(_ context.Context, _ string) *goredis.PubSub { return nil }
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run 'MapsGameBusy' -v`
Expected: both FAIL — MoveHandler returns 400 (generic branch) and JoinGameHandler returns 500, not 409.

- [ ] **Step 3: Implement the mapping**

In `internal/api/handler.go`, `MoveHandler`, replace the `ProcessMove` error branch:

```go
	g, err := h.svc.ProcessMove(r.Context(), gameID, req.PlayerID, req.MoveType, convertedPayload, req.ClientVersion)
	if err != nil {
		if errors.Is(err, service.ErrGameBusy) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		http.Error(w, err.Error(), http.StatusBadRequest) // Assume generic 400 for logic error
		return
	}
```

In `JoinGameHandler`, add alongside the `ErrGameFull` branch:

```go
		if errors.Is(err, service.ErrGameBusy) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat: map lock contention to HTTP 409 game busy"
```

---

### Task 5: CallPartnerMove domain type and no-friend

**Files:**
- Modify: `internal/game/game.go` (new type), `internal/game/rules.go` (validate + apply), `internal/api/handler.go` (ConvertPayload)
- Create: `internal/game/rules_friend_test.go`
- Test: `internal/api/handler_test.go` (append ConvertPayload cases)

**Interfaces:**
- Consumes: existing `Card`, `Game`, `ErrInvalidMove`.
- Produces: `type CallPartnerMove struct { Card *Card; NoFriend bool }` (json `card`/`no_friend`, both omitempty); `asCallPartnerMove(payload any) (CallPartnerMove, error)` package-private helper accepting `CallPartnerMove` or legacy `Card`. Tasks 6 and 10 rely on these exact names.

- [ ] **Step 1: Write the failing engine tests**

Create `internal/game/rules_friend_test.go`:

```go
package game

import (
	"errors"
	"fmt"
	"testing"
)

// callingGame returns a 5-player game in the calling phase with seat 0 as declarer.
func callingGame() *Game {
	g := New("friend-test")
	for i := range 5 {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Name: fmt.Sprintf("P%d", i), Seat: i, Hand: []Card{}, Points: []Card{}}
	}

	g.Status = PhaseCalling
	g.Declarer = 0
	g.CurrentTurn = 0
	g.Trump = Spades
	g.Contract = &Bid{PlayerID: "p0", Points: 7, Suit: Spades}

	return g
}

func TestCallPartnerWithCardSetsPartnerCard(t *testing.T) {
	t.Parallel()

	g := callingGame()
	move := CallPartnerMove{Card: &Card{Suit: Diamonds, Rank: Ace}}

	if err := g.ValidateMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerCard == nil || g.PartnerCard.Rank != Ace || g.PartnerCard.Suit != Diamonds {
		t.Fatalf("partner card not stored: %+v", g.PartnerCard)
	}

	if g.IsNoFriend {
		t.Fatal("IsNoFriend must stay false when a card is called")
	}

	if g.Status != PhasePlaying {
		t.Fatalf("expected playing, got %s", g.Status)
	}
}

func TestCallPartnerNoFriendSetsFlagAndSkipsCard(t *testing.T) {
	t.Parallel()

	g := callingGame()
	move := CallPartnerMove{NoFriend: true}

	if err := g.ValidateMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if !g.IsNoFriend {
		t.Fatal("IsNoFriend not set")
	}

	if g.PartnerCard != nil {
		t.Fatalf("partner card must be nil, got %+v", g.PartnerCard)
	}

	if g.Status != PhasePlaying {
		t.Fatalf("expected playing, got %s", g.Status)
	}
}

func TestCallPartnerRejectsBothAndNeither(t *testing.T) {
	t.Parallel()

	both := CallPartnerMove{Card: &Card{Suit: Hearts, Rank: Ace}, NoFriend: true}
	if err := callingGame().ValidateMove("p0", MoveCallPartner, both); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("both card and no_friend must be rejected, got %v", err)
	}

	neither := CallPartnerMove{}
	if err := callingGame().ValidateMove("p0", MoveCallPartner, neither); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("empty call must be rejected, got %v", err)
	}
}

func TestCallPartnerLegacyBareCardStillAccepted(t *testing.T) {
	t.Parallel()

	g := callingGame()
	card := Card{Suit: Hearts, Rank: Ace}

	if err := g.ValidateMove("p0", MoveCallPartner, card); err != nil {
		t.Fatalf("validate legacy card: %v", err)
	}

	if err := g.ApplyMove("p0", MoveCallPartner, card); err != nil {
		t.Fatalf("apply legacy card: %v", err)
	}

	if g.PartnerCard == nil || g.PartnerCard.Suit != Hearts {
		t.Fatalf("legacy card not stored: %+v", g.PartnerCard)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/game/ -run TestCallPartner -v`
Expected: COMPILE FAILURE — `CallPartnerMove` undefined.

- [ ] **Step 3: Implement the domain type and engine changes**

In `internal/game/game.go`, after `PlayCardMove`:

```go
// CallPartnerMove represents the declarer's friend call: either a card
// (whose holder becomes the secret partner) or no_friend to play alone.
type CallPartnerMove struct {
	Card     *Card `json:"card,omitempty"`
	NoFriend bool  `json:"no_friend,omitempty"`
}
```

In `internal/game/rules.go`, add the helper and replace `validateCallPartner`:

```go
// asCallPartnerMove normalizes the two accepted payload shapes.
func asCallPartnerMove(payload any) (CallPartnerMove, error) {
	switch v := payload.(type) {
	case CallPartnerMove:
		return v, nil
	case Card:
		return CallPartnerMove{Card: &v}, nil
	default:
		return CallPartnerMove{}, errors.New("invalid payload for partner call")
	}
}

// validateCallPartner
// Payload: CallPartnerMove (or legacy Card).
func (g *Game) validateCallPartner(p *Player, payload any) error {
	if g.Status != PhaseCalling {
		return fmt.Errorf("%w: not in calling phase", ErrInvalidMove)
	}

	if g.Players[g.Declarer].ID != p.ID {
		return fmt.Errorf("%w: only declarer call partner", ErrInvalidMove)
	}

	move, err := asCallPartnerMove(payload)
	if err != nil {
		return err
	}

	if move.Card != nil && move.NoFriend {
		return fmt.Errorf("%w: choose a card or no_friend, not both", ErrInvalidMove)
	}

	if move.Card == nil && !move.NoFriend {
		return fmt.Errorf("%w: call_partner requires a card or no_friend", ErrInvalidMove)
	}

	return nil
}
```

Replace the `MoveCallPartner` case in `ApplyMove`:

```go
	case MoveCallPartner:
		move, err := asCallPartnerMove(payload)
		if err != nil {
			return err
		}

		if move.NoFriend {
			g.IsNoFriend = true
			g.PartnerCard = nil
		} else {
			g.PartnerCard = move.Card
		}

		g.Status = PhasePlaying
		// Start playing
		g.CurrentTurn = g.Declarer // Declarer leads first trick
		g.Tricks = append(g.Tricks, Trick{Cards: []PlayedCard{}})
```

In `internal/api/handler.go` `ConvertPayload`, replace the `MoveCallPartner` case:

```go
	case game.MoveCallPartner:
		var move game.CallPartnerMove
		if err := json.Unmarshal(data, &move); err != nil {
			return nil, err
		}

		if move.Card == nil && !move.NoFriend {
			// Legacy shape: the payload is the card itself.
			var card game.Card
			if err := json.Unmarshal(data, &card); err == nil && card.Rank != "" {
				return game.CallPartnerMove{Card: &card}, nil
			}

			return nil, errors.New("call_partner requires a card or no_friend")
		}

		return move, nil
```

- [ ] **Step 4: Write and run the ConvertPayload tests**

Append to `internal/api/handler_test.go`:

```go
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
```

Run: `go test ./internal/game/ -run TestCallPartner -v && go test ./internal/api/ -run TestConvertPayloadCallPartner -v`
Expected: all PASS.

- [ ] **Step 5: Run the full unit suite and commit**

Run: `go test ./internal/... && go vet ./...`
Expected: PASS.

```bash
git add internal/game/ internal/api/
git commit -m "feat: call_partner supports no_friend and typed payload"
```

---

### Task 6: Partner reveal and scoring fix

**Files:**
- Modify: `internal/game/rules.go` (ApplyMove MovePlayCard; CalculateFinalScore), `internal/game/game.go` (Scores comment)
- Test: `internal/game/rules_friend_test.go` (append)

**Interfaces:**
- Consumes: Task 5's calling flow.
- Produces: `PartnerSeat` set when the called card is played; `CalculateFinalScore` returns `friendScore == 0` when `PartnerSeat < 0`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/game/rules_friend_test.go`:

```go
// playingGameWithPartnerCard returns a game mid-play (trick 2 open) where the
// declarer (seat 0) has called ♦A and it sits in seat 2's hand.
func playingGameWithPartnerCard() *Game {
	g := callingGame()
	g.Status = PhasePlaying
	g.PartnerCard = &Card{Suit: Diamonds, Rank: Ace}
	g.Tricks = []Trick{
		{Cards: make([]PlayedCard, 5), LeadSuit: Clubs, Winner: 2},
		{Cards: []PlayedCard{}},
	}
	g.CurrentTurn = 2
	g.Players[2].Hand = []Card{{Suit: Diamonds, Rank: Ace}, {Suit: Clubs, Rank: Two}}

	return g
}

func TestPlayingCalledCardRevealsPartner(t *testing.T) {
	t.Parallel()

	g := playingGameWithPartnerCard()
	move := PlayCardMove{Card: Card{Suit: Diamonds, Rank: Ace}}

	if err := g.ValidateMove("p2", MovePlayCard, move); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if err := g.ApplyMove("p2", MovePlayCard, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != 2 {
		t.Fatalf("expected partner seat 2, got %d", g.PartnerSeat)
	}
}

func TestDeclarerPlayingOwnCalledCardIsSelfPartner(t *testing.T) {
	t.Parallel()

	g := playingGameWithPartnerCard()
	g.CurrentTurn = 0
	g.Players[0].Hand = []Card{{Suit: Diamonds, Rank: Ace}}
	g.Players[2].Hand = []Card{{Suit: Clubs, Rank: Two}}

	move := PlayCardMove{Card: Card{Suit: Diamonds, Rank: Ace}}
	if err := g.ApplyMove("p0", MovePlayCard, move); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if g.PartnerSeat != 0 {
		t.Fatalf("expected self-partner seat 0, got %d", g.PartnerSeat)
	}
}

func TestUnplayedCalledCardScoresDeclarerAloneWithoutDoubling(t *testing.T) {
	t.Parallel()

	g := callingGame()
	g.PartnerCard = &Card{Suit: Diamonds, Rank: Ace} // stayed in the kitty
	g.PartnerSeat = -1
	g.IsNoFriend = false
	// Declarer alone wins exactly the 7-trick contract.
	g.Tricks = make([]Trick, 10)
	for i := range g.Tricks {
		if i < 7 {
			g.Tricks[i].Winner = 0
		} else {
			g.Tricks[i].Winner = 3
		}
	}

	declarer, friend := g.CalculateFinalScore()
	if declarer != 70 {
		t.Fatalf("expected 70 (no x2 doubling), got %v", declarer)
	}

	if friend != 0 {
		t.Fatalf("expected friend score 0 with no revealed partner, got %v", friend)
	}
}

func TestScoringCountsRevealedPartnerTricks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		contract          int
		declarerTricks    int
		partnerTricks     int
		noTrump, noFriend bool
		wantDeclarer      float64
		wantFriend        float64
	}{
		{name: "exact contract split", contract: 7, declarerTricks: 4, partnerTricks: 3, wantDeclarer: 70, wantFriend: 35},
		{name: "overtricks", contract: 7, declarerTricks: 5, partnerTricks: 4, wantDeclarer: 80, wantFriend: 40},
		{name: "down one", contract: 7, declarerTricks: 4, partnerTricks: 2, wantDeclarer: -70, wantFriend: -35},
		{name: "down two adds penalty", contract: 7, declarerTricks: 3, partnerTricks: 2, wantDeclarer: -75, wantFriend: -37.5},
		{name: "no trump doubles", contract: 7, declarerTricks: 4, partnerTricks: 3, noTrump: true, wantDeclarer: 140, wantFriend: 70},
		{name: "ten bid doubles", contract: 10, declarerTricks: 6, partnerTricks: 4, wantDeclarer: 200, wantFriend: 100},
		{name: "cap at 800", contract: 10, declarerTricks: 10, partnerTricks: 0, noTrump: true, noFriend: true, wantDeclarer: 800, wantFriend: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := callingGame()
			g.Contract = &Bid{PlayerID: "p0", Points: tc.contract, Suit: Spades, IsNoTrump: tc.noTrump}
			if tc.noTrump {
				g.Contract.Suit = None
			}

			g.IsNoFriend = tc.noFriend
			g.PartnerSeat = 1
			if tc.noFriend {
				g.PartnerSeat = -1
			}

			g.Tricks = make([]Trick, 10)
			seat := 0

			for i := range g.Tricks {
				switch {
				case seat < tc.declarerTricks:
					g.Tricks[i].Winner = 0
				case seat < tc.declarerTricks+tc.partnerTricks:
					g.Tricks[i].Winner = 1
				default:
					g.Tricks[i].Winner = 3
				}
				seat++
			}

			declarer, friend := g.CalculateFinalScore()
			if declarer != tc.wantDeclarer || friend != tc.wantFriend {
				t.Fatalf("got (%v, %v), want (%v, %v)", declarer, friend, tc.wantDeclarer, tc.wantFriend)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/game/ -run 'TestPlayingCalledCard|TestDeclarerPlayingOwn|TestUnplayedCalledCard|TestScoringCounts' -v`
Expected: FAIL — `PartnerSeat` stays -1 after playing the called card (reveal tests), and the "down two"/split expectations expose the current partner-blind scoring.

- [ ] **Step 3: Implement reveal and scoring guard**

In `internal/game/rules.go`, `ApplyMove` `MovePlayCard` case, insert immediately after the `g.Tricks[idx].Cards = append(...)` block (before the lead-suit handling):

```go
		// Reveal the mystery friend the moment the called card hits the table.
		if g.PartnerCard != nil && card.Suit == g.PartnerCard.Suit && card.Rank == g.PartnerCard.Rank {
			g.PartnerSeat = p.Seat
		}
```

In `CalculateFinalScore`, replace the friend-score tail:

```go
	friendScore := score / 2.0
	if g.IsNoFriend || g.PartnerSeat < 0 {
		friendScore = 0 // No revealed friend to share with.
	}
```

In `internal/game/game.go`, update the `Scores` field comment:

```go
	Scores map[string]int `json:"scores"` // Final round scores: declarer full, revealed partner half, others 0. Card points live in Player.Points.
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/game/ -v`
Expected: all PASS, including pre-existing rules tests.

- [ ] **Step 5: Commit**

```bash
git add internal/game/
git commit -m "fix: reveal partner when called card is played and score the team"
```

---

### Task 7: Joker lead with called_suit

**Files:**
- Modify: `internal/game/game.go` (PlayCardMove), `internal/game/rules.go` (validatePlayCard, ApplyMove lead handling)
- Create: `internal/game/rules_joker_test.go`

**Interfaces:**
- Consumes: existing `suitRank` map (rules.go), `Joker` rank.
- Produces: `PlayCardMove.CalledSuit Suit` (json `called_suit,omitempty`); leading Joker requires a real suit; `Trick.LeadSuit` set from it.

- [ ] **Step 1: Write the failing tests**

Create `internal/game/rules_joker_test.go`:

```go
package game

import (
	"errors"
	"fmt"
	"testing"
)

// jokerLeadGame returns a game where seat 0 leads trick 2 holding the Joker.
func jokerLeadGame() *Game {
	g := New("joker-test")
	for i := range 5 {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Seat: i, Hand: []Card{}, Points: []Card{}}
	}

	g.Status = PhasePlaying
	g.Declarer = 0
	g.Trump = Spades
	g.Tricks = []Trick{
		{Cards: make([]PlayedCard, 5), LeadSuit: Clubs, Winner: 0},
		{Cards: []PlayedCard{}},
	}
	g.CurrentTurn = 0
	g.Players[0].Hand = []Card{{Suit: None, Rank: Joker}, {Suit: Clubs, Rank: Two}}
	g.Players[1].Hand = []Card{{Suit: Hearts, Rank: King}, {Suit: Clubs, Rank: Three}}

	return g
}

func TestJokerLeadRequiresCalledSuit(t *testing.T) {
	t.Parallel()

	g := jokerLeadGame()
	move := PlayCardMove{Card: Card{Suit: None, Rank: Joker}}

	if err := g.ValidateMove("p0", MovePlayCard, move); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("joker lead without called_suit must be rejected, got %v", err)
	}
}

func TestJokerLeadSetsLeadSuitAndForcesFollow(t *testing.T) {
	t.Parallel()

	g := jokerLeadGame()
	move := PlayCardMove{Card: Card{Suit: None, Rank: Joker}, CalledSuit: Hearts}

	if err := g.ValidateMove("p0", MovePlayCard, move); err != nil {
		t.Fatalf("validate joker lead: %v", err)
	}

	if err := g.ApplyMove("p0", MovePlayCard, move); err != nil {
		t.Fatalf("apply joker lead: %v", err)
	}

	if got := g.Tricks[1].LeadSuit; got != Hearts {
		t.Fatalf("lead suit not taken from called_suit: %s", got)
	}

	// Seat 1 holds a heart, so a club is an illegal follow.
	follow := PlayCardMove{Card: Card{Suit: Clubs, Rank: Three}}
	if err := g.ValidateMove("p1", MovePlayCard, follow); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("must follow the called suit, got %v", err)
	}

	legal := PlayCardMove{Card: Card{Suit: Hearts, Rank: King}}
	if err := g.ValidateMove("p1", MovePlayCard, legal); err != nil {
		t.Fatalf("heart follow must be legal: %v", err)
	}
}

func TestCalledSuitRejectedOffJokerLead(t *testing.T) {
	t.Parallel()

	g := jokerLeadGame()

	// Non-joker lead with called_suit.
	lead := PlayCardMove{Card: Card{Suit: Clubs, Rank: Two}, CalledSuit: Hearts}
	if err := g.ValidateMove("p0", MovePlayCard, lead); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("called_suit on a non-joker lead must be rejected, got %v", err)
	}

	// Following with called_suit.
	if err := g.ApplyMove("p0", MovePlayCard, PlayCardMove{Card: Card{Suit: Clubs, Rank: Two}}); err != nil {
		t.Fatalf("setup lead: %v", err)
	}

	follow := PlayCardMove{Card: Card{Suit: Clubs, Rank: Three}, CalledSuit: Hearts}
	if err := g.ValidateMove("p1", MovePlayCard, follow); !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("called_suit while following must be rejected, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/game/ -run 'TestJokerLead|TestCalledSuit' -v`
Expected: COMPILE FAILURE — `CalledSuit` field undefined.

- [ ] **Step 3: Implement**

In `internal/game/game.go`, replace `PlayCardMove`:

```go
// PlayCardMove represents the payload for playing a card.
type PlayCardMove struct {
	Card       Card `json:"card"`
	CallJoker  bool `json:"call_joker"`
	CalledSuit Suit `json:"called_suit,omitempty"` // required when leading the Joker
}
```

In `internal/game/rules.go` `validatePlayCard`, inside the leading branch (`if len(t.Cards) == 0 {`), add before the existing joker-caller checks:

```go
		// Joker lead must declare the suit followers owe; called_suit is
		// meaningless on any other play.
		if card.Rank == Joker {
			if _, ok := suitRank[move.CalledSuit]; !ok {
				return fmt.Errorf("%w: joker lead requires called_suit", ErrInvalidMove)
			}
		} else if move.CalledSuit != "" {
			return fmt.Errorf("%w: called_suit only valid when leading the joker", ErrInvalidMove)
		}
```

and in the following branch (after the leading branch's `return nil`), add at its start:

```go
	if move.CalledSuit != "" {
		return fmt.Errorf("%w: called_suit only valid when leading the joker", ErrInvalidMove)
	}
```

In `ApplyMove` `MovePlayCard`, replace the lead-suit block (the one with the long design comment about Joker leads):

```go
		// Set Lead Suit if first card
		if len(g.Tricks[idx].Cards) == 1 {
			g.Tricks[idx].LeadSuit = card.Suit
			if card.Rank == Joker {
				g.Tricks[idx].LeadSuit = move.CalledSuit
			}

			// Handle Joker Caller
			if move.CallJoker && g.IsJokerCaller(card) {
				g.Tricks[idx].JokerCalled = true
			}
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/game/ -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/
git commit -m "feat: joker lead declares called_suit that followers must follow"
```

---

### Task 8: Bidding cleanup — dead branch and all-pass redeal

**Files:**
- Modify: `internal/game/rules.go` (ApplyMove MoveBid + MovePass)
- Create: `internal/game/rules_redeal_test.go`

**Interfaces:**
- Consumes: `Game.Start()` (game.go — shuffles, deals 10/10/10/10/10+3, sets `PhaseBidding`, `CurrentTurn = 0`).
- Produces: all-pass now redeals in place.

- [ ] **Step 1: Write the failing test**

Create `internal/game/rules_redeal_test.go`:

```go
package game

import (
	"fmt"
	"testing"
)

func TestAllPassRedealsInsteadOfFinishing(t *testing.T) {
	t.Parallel()

	g := New("redeal-test")
	for i := range 5 {
		g.Players[i] = &Player{ID: fmt.Sprintf("p%d", i), Seat: i, Hand: []Card{}, Points: []Card{}}
	}

	g.Start()

	firstHands := make([][]Card, 5)
	for i, p := range g.Players {
		firstHands[i] = append([]Card{}, p.Hand...)
	}

	versionBefore := g.Version

	for i := range 5 {
		playerID := fmt.Sprintf("p%d", i)
		if err := g.ValidateMove(playerID, MovePass, nil); err != nil {
			t.Fatalf("pass %d validate: %v", i, err)
		}

		if err := g.ApplyMove(playerID, MovePass, nil); err != nil {
			t.Fatalf("pass %d apply: %v", i, err)
		}
	}

	if g.Status != PhaseBidding {
		t.Fatalf("expected fresh bidding phase after all-pass, got %s", g.Status)
	}

	if len(g.PassedPlayers) != 0 {
		t.Fatalf("passes must be cleared, got %v", g.PassedPlayers)
	}

	if g.CurrentBid != nil || g.Declarer != -1 || g.Contract != nil {
		t.Fatalf("bidding state must be reset: bid=%v declarer=%d contract=%v", g.CurrentBid, g.Declarer, g.Contract)
	}

	for i, p := range g.Players {
		if len(p.Hand) != 10 {
			t.Fatalf("player %d has %d cards after redeal", i, len(p.Hand))
		}
	}

	if len(g.Kitty) != 3 {
		t.Fatalf("kitty must be redealt with 3 cards, got %d", len(g.Kitty))
	}

	if g.Version <= versionBefore {
		t.Fatalf("version must advance across redeal: %d -> %d", versionBefore, g.Version)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game/ -run TestAllPassRedeals -v`
Expected: FAIL — status is `finished` after five passes.

- [ ] **Step 3: Implement**

In `internal/game/rules.go` `ApplyMove`:

Delete the dead pass-in-bid branch in the `MoveBid` case — remove:

```go
		// If pass
		if bid.Points == 0 {
			p := g.GetPlayer(playerID)
			if p != nil {
				g.PassedPlayers[p.Seat] = true
			}
		} else {
```

keeping the body of the `else` (unindented) — `validateBid` guarantees points 3–10, so the bid always applies:

```go
		bid.PlayerID = playerID // Ensure playerID is set

		g.CurrentBid = &bid
		g.Declarer = p.Seat                  // Potential declarer
		g.PassedPlayers = make(map[int]bool) // Clear passes when someone bids

		g.CurrentTurn = (g.CurrentTurn + 1) % 5
```

In the `MovePass` case, replace the `else if len(g.PassedPlayers) == 5` branch:

```go
		} else if len(g.PassedPlayers) == 5 {
			// Everyone passed: throw the hand in and redeal.
			g.Bids = nil
			g.CurrentBid = nil
			g.Contract = nil
			g.Declarer = -1
			g.PassedPlayers = make(map[int]bool)
			g.PartnerCard = nil
			g.PartnerSeat = -1
			g.IsNoFriend = false
			g.Trump = ""
			g.Tricks = make([]Trick, 0)
			g.Start()
		}
```

- [ ] **Step 4: Run the full unit suite**

Run: `go test ./internal/... && go vet ./...`
Expected: all PASS (the gherkin-style rules tests in `rules_gherkin_test.go` must stay green).

- [ ] **Step 5: Commit**

```bash
git add internal/game/
git commit -m "fix: all-pass redeals the hand; drop dead pass-in-bid branch"
```

---

### Task 9: API documentation updates

**Files:**
- Modify: `docs/API_DOCUMENTATION.md`

**Interfaces:** none (docs only). Documents exactly what Tasks 4–8 shipped.

- [ ] **Step 1: Update the move payload sections**

In `docs/API_DOCUMENTATION.md`:

Replace the **Call Partner** payload section:

```markdown
### 3. Call Partner
Either call a card (its holder becomes the secret partner):
`{"card": {"suit": "hearts", "rank": "A"}}`
or play alone for doubled score:
`{"no_friend": true}`
Exactly one of the two must be present. (A legacy bare card object is still accepted.)
```

Replace the **Play Card** payload section:

```markdown
### 4. Play Card
```json
{
  "card": {"suit": "clubs", "rank": "10"},
  "call_joker": false,     // true only when leading the Joker Caller (3-Clubs)
  "called_suit": "hearts"  // REQUIRED when leading the Joker; forbidden otherwise
}
```
When the Joker leads, `called_suit` becomes the trick's lead suit and other players must follow it.
```

Add to the **Scoring** section:

```markdown
- **`scores` field**: final round scores — declarer full score, revealed partner half, all other players 0. Card points taken in tricks are in each player's `points` array.
- **All-pass**: if all five players pass, the hand is thrown in and redealt (status returns to `bidding` with fresh hands).
```

Add under **Submit Move**:

```markdown
**Errors**: `409 Conflict` with body `game busy` when the game's move lock is contended — retry the request. `400` with `stale version` when `client_version` does not match the current game version — refresh state and retry.
```

- [ ] **Step 2: Commit**

```bash
git add docs/API_DOCUMENTATION.md
git commit -m "docs: call_partner, called_suit, scores semantics, busy/stale errors"
```

---

### Task 10: E2E — friend reveal and no-friend scenarios

**Files:**
- Create: `tests/e2e/features/friend.feature`
- Modify: `tests/e2e/e2e_test.go`

**Interfaces:**
- Consumes: existing steps (`5 authenticated players`, `creates a .*game`, `joins seat`, `bids`, `passes`, `discards`, status assertions) and helpers `move`, `refreshState`, `findLegalCard`, `playOutGame`.
- Produces: steps `declares no friend`, `all remaining tricks are played out legally`, `the partner seat should match whoever played the called card`, `the final scores should follow the declarer-partner split`, `the game should have no friend`; `apiFeature.calledCard` field.

**Prerequisite:** full stack running with the new server build: `docker compose up -d --build`.

- [ ] **Step 1: Write the feature file**

Create `tests/e2e/features/friend.feature`:

```gherkin
Feature: Mystery Friend
  The declarer's called card reveals the partner when played,
  and no_friend lets the declarer play alone.

  Scenario: Partner is revealed when the called card is played
    Given 5 authenticated players: "Ann", "Ben", "Cid", "Dot", "Eli"
    And "Ann" creates a high-stakes game "friend-1"
    When "Ann" joins seat 0 of game "friend-1"
    And "Ben" joins seat 1 of game "friend-1"
    And "Cid" joins seat 2 of game "friend-1"
    And "Dot" joins seat 3 of game "friend-1"
    And "Eli" joins seat 4 of game "friend-1"
    Then the game "friend-1" status should be "bidding"
    When "Ann" bids 5 "spades"
    And "Ben" passes
    And "Cid" passes
    And "Dot" passes
    And "Eli" passes
    Then the game "friend-1" status should be "exchanging"
    When "Ann" discards 3 least powerful cards
    And "Ann" calls the "Ace of Hearts" as the friend
    Then the game "friend-1" status should be "playing"
    When all remaining tricks are played out legally
    Then the game "friend-1" status should be "finished"
    And the partner seat should match whoever played the called card
    And the final scores should follow the declarer-partner split

  Scenario: Declarer plays alone with no friend
    Given 5 authenticated players: "Fay", "Gus", "Hal", "Ivy", "Jon"
    And "Fay" creates a high-stakes game "friend-2"
    When "Fay" joins seat 0 of game "friend-2"
    And "Gus" joins seat 1 of game "friend-2"
    And "Hal" joins seat 2 of game "friend-2"
    And "Ivy" joins seat 3 of game "friend-2"
    And "Jon" joins seat 4 of game "friend-2"
    Then the game "friend-2" status should be "bidding"
    When "Fay" bids 5 "spades"
    And "Gus" passes
    And "Hal" passes
    And "Ivy" passes
    And "Jon" passes
    Then the game "friend-2" status should be "exchanging"
    When "Fay" discards 3 least powerful cards
    And "Fay" declares no friend
    Then the game "friend-2" status should be "playing"
    And the game should have no friend
    When all remaining tricks are played out legally
    Then the game "friend-2" status should be "finished"
    And the final scores should follow the declarer-partner split
```

- [ ] **Step 2: Add the field, fix findLegalCard, add steps**

In `tests/e2e/e2e_test.go`:

Add to `apiFeature` struct:

```go
	calledCard *game.Card
```

Reset it in the `ctx.Before` hook alongside the other fields:

```go
		api.calledCard = nil
```

In `findLegalCard`, in the **leading** loop (the `len(currentTrick.Cards) == 0` branch), skip the Joker so bots never owe a `called_suit` (add as the first statement in the `for _, c := range p.Hand` loop):

```go
			if c.Rank == game.Joker && len(p.Hand) > 1 {
				continue // leading the Joker needs called_suit; keep bots simple
			}
```

Replace the friend-call step registration (currently ignores the card name and hardcodes ♥A — keep ♥A but record it):

```go
	ctx.Step(`^"([^"]*)" calls the "([^"]*)" as the friend$`, func(u, _ string) error {
		card := game.Card{Suit: game.Hearts, Rank: game.Ace}
		api.calledCard = &card

		return api.move(u, game.MoveCallPartner, game.CallPartnerMove{Card: &card})
	})
```

Register the new steps (inside `InitializeScenario`, near the other game steps):

```go
	ctx.Step(`^"([^"]*)" declares no friend$`, func(u string) error {
		return api.move(u, game.MoveCallPartner, map[string]any{"no_friend": true})
	})
	ctx.Step(`^all remaining tricks are played out legally$`, func() error { return api.playOutGame() })
	ctx.Step(`^the game should have no friend$`, func() error {
		if err := api.refreshState(); err != nil {
			return err
		}

		if !api.game.IsNoFriend {
			return errors.New("expected is_no_friend true")
		}

		return nil
	})
	ctx.Step(`^the partner seat should match whoever played the called card$`, func() error {
		if err := api.refreshState(); err != nil {
			return err
		}

		playedBy := -1

		for _, trick := range api.game.Tricks {
			for _, pc := range trick.Cards {
				if api.calledCard != nil && pc.Card.Suit == api.calledCard.Suit && pc.Card.Rank == api.calledCard.Rank {
					playedBy = pc.Seat
				}
			}
		}

		if api.game.PartnerSeat != playedBy {
			return fmt.Errorf("partner seat %d, but called card was played by seat %d", api.game.PartnerSeat, playedBy)
		}

		return nil
	})
	ctx.Step(`^the final scores should follow the declarer-partner split$`, func() error {
		if err := api.refreshState(); err != nil {
			return err
		}

		declarer := api.game.Players[api.game.Declarer]
		declarerScore := api.game.Scores[declarer.ID]

		if declarerScore == 0 {
			return errors.New("declarer round score must be non-zero")
		}

		if seat := api.game.PartnerSeat; seat >= 0 && seat != api.game.Declarer {
			partnerScore := api.game.Scores[api.game.Players[seat].ID]
			if diff := declarerScore - 2*partnerScore; diff < -1 || diff > 1 {
				return fmt.Errorf("partner score %d is not half of declarer %d", partnerScore, declarerScore)
			}
		}

		for _, p := range api.game.Players {
			if p == nil || p.Seat == api.game.Declarer || p.Seat == api.game.PartnerSeat {
				continue
			}

			if s := api.game.Scores[p.ID]; s != 0 {
				return fmt.Errorf("non-team player %d has score %d, want 0", p.Seat, s)
			}
		}

		return nil
	})
```

- [ ] **Step 3: Rebuild the stack and run the E2E suite**

```bash
docker compose up -d --build
go test -v -tags=integration ./tests/e2e/... -run TestFeatures 2>&1 | tail -20
```

Expected: all scenarios PASS, including the pre-existing marathon (its friend call now goes through `CallPartnerMove` and its plays avoid Joker leads).

- [ ] **Step 4: Run everything one last time**

```bash
go test ./internal/... && go vet ./... && go test -tags=integration ./internal/store/redis/... && go test -v -tags=integration ./tests/e2e/... 2>&1 | tail -5
```

Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add tests/e2e/
git commit -m "test: e2e friend reveal and no-friend scenarios"
```

---

## Spec coverage map

- §2 lock/token/backoff → Task 1; CAS + CheckVersion removal → Task 2; service flow → Task 3; 409 mapping → Task 4 (F1, F2, F3).
- §3 call_partner payload + no-friend → Task 5 (F5); partner reveal + kitty case + Scores comment → Task 6 (F4).
- §4 joker `called_suit` → Task 7 (F6).
- §5 dead branch + redeal → Task 8 (F7, F8).
- §6 error surface → Tasks 3–4.
- §7 testing → Tasks 1–2 (integration), 5–8 (unit), 10 (E2E).
- Docs (§3 Scores, payload shapes) → Task 9.
- Cross-repo frontend adaptation: explicitly out of this plan (spec §7 note).
