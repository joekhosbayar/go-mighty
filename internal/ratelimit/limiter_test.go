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
