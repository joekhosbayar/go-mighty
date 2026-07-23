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
