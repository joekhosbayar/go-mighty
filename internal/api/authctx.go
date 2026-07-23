package api

import (
	"context"
	"net/http"

	"github.com/joekhosbayar/go-mighty/internal/service"
)

// claimsCtxKey is unexported so nothing outside this package can collide with
// or forge the context value.
type claimsCtxKey struct{}

// WithClaims returns a copy of ctx carrying the authenticated identity.
func WithClaims(ctx context.Context, claims *service.AuthClaims) context.Context {
	return context.WithValue(ctx, claimsCtxKey{}, claims)
}

// ClaimsFromContext returns the identity stored by RequireAuth, if any.
func ClaimsFromContext(ctx context.Context) (*service.AuthClaims, bool) {
	claims, ok := ctx.Value(claimsCtxKey{}).(*service.AuthClaims)
	return claims, ok && claims != nil
}

// RequireAuth validates the bearer token once, up front, and stores the
// resulting claims in the request context. Middleware layered beneath it
// (rate limiting) can then key on the Cognito sub without re-validating, and
// handlers keep working unchanged because authenticate reads the context
// first.
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := h.authenticate(r)
		if err != nil {
			writeAuthError(w, err)
			return
		}

		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
	})
}
