package ratelimit

import (
	"context"
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

// TestAllowClampsTimestampAgainstOutOfOrderArrival guards against the Lua
// script writing `ts` backward. Timestamps originate on the Go side, so two
// concurrent requests for the same key can reach Redis out of timestamp
// order under ordinary goroutine/network jitter. If the script ever wrote
// `ts = now_ms` unconditionally, a later-arriving-but-earlier-stamped
// request would pull `ts` backward, and the next legitimate request would
// compute an inflated `elapsed` against that too-early `ts`, over-crediting
// tokens.
func TestAllowClampsTimestampAgainstOutOfOrderArrival(t *testing.T) {
	limiter, clock := newTestLimiter(t)
	rule := Rule{Capacity: 2, RefillPerSec: 1}
	t0 := *clock

	// Consume both tokens at t0; the bucket is now empty with ts == t0.
	if d := limiter.Allow(t.Context(), "k", rule); !d.Allowed {
		t.Fatal("expected the first call at t0 to be allowed")
	}
	if d := limiter.Allow(t.Context(), "k", rule); !d.Allowed {
		t.Fatal("expected the second call at t0 to be allowed")
	}

	// An out-of-order arrival stamped 10s before t0 must be denied (the
	// bucket is empty) and must not pull the stored ts backward.
	*clock = t0.Add(-10 * time.Second)
	if d := limiter.Allow(t.Context(), "k", rule); d.Allowed {
		t.Fatal("expected the out-of-order call to be denied")
	}

	// A legitimate call 1s after t0 should see exactly one token refilled
	// (t0 to t0+1s), not eleven (t0-10s to t0+1s).
	*clock = t0.Add(1 * time.Second)
	if d := limiter.Allow(t.Context(), "k", rule); !d.Allowed {
		t.Fatal("expected exactly one token to have refilled 1s after t0")
	}
	if d := limiter.Allow(t.Context(), "k", rule); d.Allowed {
		t.Fatal("expected only one token to have refilled; the second call should be denied")
	}
}

// malformedReplyScripter is a redis.Scripter whose EvalSha (the method
// Script.Run tries first) always returns a reply shaped like the bucket
// script's {allowed, retry_ms} pair but with the wrong element types, to
// exercise Allow's decode path when the type assertions fail.
type malformedReplyScripter struct{}

func (malformedReplyScripter) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	return redis.NewCmdResult([]interface{}{"not-an-int", "also-not-an-int"}, nil)
}

func (m malformedReplyScripter) EvalSha(ctx context.Context, sha1 string, keys []string, args ...interface{}) *redis.Cmd {
	return m.Eval(ctx, "", keys, args...)
}

func (malformedReplyScripter) EvalRO(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	return redis.NewCmdResult(nil, nil)
}

func (malformedReplyScripter) EvalShaRO(ctx context.Context, sha1 string, keys []string, args ...interface{}) *redis.Cmd {
	return redis.NewCmdResult(nil, nil)
}

func (malformedReplyScripter) ScriptExists(ctx context.Context, hashes ...string) *redis.BoolSliceCmd {
	return redis.NewBoolSliceCmd(ctx)
}

func (malformedReplyScripter) ScriptLoad(ctx context.Context, script string) *redis.StringCmd {
	return redis.NewStringCmd(ctx)
}

// TestAllowFailsOpenOnMalformedScriptReply guards the decode path in Allow:
// if the script reply's elements are ever not the expected int64s, Allow
// must fail open (matching the neighbouring len(res) != 2 check) rather
// than silently zero-valuing into Decision{Allowed: false}, which would
// fail closed and contradict the fail-open invariant.
func TestAllowFailsOpenOnMalformedScriptReply(t *testing.T) {
	limiter := New(malformedReplyScripter{})

	d := limiter.Allow(t.Context(), "k", Rule{Capacity: 1, RefillPerSec: 1})
	if !d.Allowed {
		t.Fatal("a malformed script reply must fail open")
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

// TestAllowFailsOpenWhenLimiterIsNil covers one of the three fail-open cases
// documented on Allow: a nil *Limiter receiver must not panic and must
// return Allowed.
func TestAllowFailsOpenWhenLimiterIsNil(t *testing.T) {
	var limiter *Limiter

	d := limiter.Allow(t.Context(), "k", Rule{Capacity: 1, RefillPerSec: 1})
	if !d.Allowed {
		t.Fatal("a nil limiter must fail open")
	}
}

// TestAllowFailsOpenWhenRuleIsNonsensical covers the second fail-open case:
// a rule with a non-positive Capacity or RefillPerSec must not be enforced
// (there is nothing sensible to enforce), so it fails open rather than
// denying every request.
func TestAllowFailsOpenWhenRuleIsNonsensical(t *testing.T) {
	limiter, _ := newTestLimiter(t)

	cases := map[string]Rule{
		"zero capacity":        {Capacity: 0, RefillPerSec: 1},
		"negative capacity":    {Capacity: -1, RefillPerSec: 1},
		"zero refill":          {Capacity: 1, RefillPerSec: 0},
		"negative refill":      {Capacity: 1, RefillPerSec: -1},
		"zero capacity+refill": {Capacity: 0, RefillPerSec: 0},
	}

	for name, rule := range cases {
		t.Run(name, func(t *testing.T) {
			d := limiter.Allow(t.Context(), "k", rule)
			if !d.Allowed {
				t.Fatalf("rule %+v must fail open, got denied", rule)
			}
		})
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
