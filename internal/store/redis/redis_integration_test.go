//go:build integration

package redis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/game"
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

func TestSaveGameCAS(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	g := game.New(fmt.Sprintf("cas-test-%s-%d", t.Name(), time.Now().UnixNano()))

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
	g := game.New(fmt.Sprintf("concurrency-test-%s-%d", t.Name(), time.Now().UnixNano()))

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
