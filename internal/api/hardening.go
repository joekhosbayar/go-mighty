package api

import (
	"net/http"
)

// MaxBodyBytes caps request bodies at 64KB (spec Section 3, Layer 1). No
// legitimate request to this API is anywhere near that: the largest is a
// game-config change of a few hundred bytes.
const MaxBodyBytes int64 = 64 << 10

// BodyLimitMiddleware enforces MaxBodyBytes twice over: a declared
// Content-Length above the cap is refused outright with 413, and anything
// else is wrapped so a chunked or lying client still can't stream more than
// the cap into memory.
//
// It is safe on WebSocket upgrades: those carry no body, and wrapping
// r.Body does not interfere with hijacking the connection.
func BodyLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > MaxBodyBytes {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
		}

		next.ServeHTTP(w, r)
	})
}
