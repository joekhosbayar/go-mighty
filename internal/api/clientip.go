package api

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the caller's IP address.
//
// Behind the Caddy container every request's RemoteAddr is the proxy's
// address, which would collapse all clients into one bucket, so when
// trustProxy is set the first X-Forwarded-For entry wins. That header is
// trivially forged by a direct caller, which is why trusting it is opt-in and
// only enabled in the deployment where the security group makes Caddy the
// only possible source of traffic.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
