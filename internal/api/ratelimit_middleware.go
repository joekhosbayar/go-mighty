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
