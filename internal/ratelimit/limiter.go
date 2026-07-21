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
else
  -- now_ms arrived out of order (an earlier-stamped request reached Redis
  -- after a later-stamped one). Clamp ts to its previous value instead of
  -- writing it backward, or the next legitimate request would compute an
  -- inflated elapsed against a too-early ts and over-credit tokens.
  now_ms = ts
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

	allowed, allowedOK := res[0].(int64)
	retryMS, retryOK := res[1].(int64)
	if !allowedOK || !retryOK {
		log.Warn().Str("key", key).Msg("Unexpected rate limiter reply types, allowing request")
		return Decision{Allowed: true}
	}

	return Decision{
		Allowed:    allowed == 1,
		RetryAfter: time.Duration(retryMS) * time.Millisecond,
	}
}
