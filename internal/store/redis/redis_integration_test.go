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
