package api

import (
	"strings"

	"github.com/joekhosbayar/go-mighty/internal/ratelimit"
)

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

// WithWSMessageRate caps inbound WebSocket messages per connection at perSec
// messages/second with a burst of burst (spec Section 3: 10/sec, burst 20).
// Zero or negative values disable the cap.
func WithWSMessageRate(perSec, burst float64) Option {
	return func(h *Handler) {
		h.wsMessagesPerSec = perSec
		h.wsMessageBurst = burst
	}
}
