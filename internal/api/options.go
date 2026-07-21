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
