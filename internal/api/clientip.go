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
// only safe under two conditions holding together: the security group makes
// Caddy the only possible source of traffic (controls who can reach the
// port), and Caddy's reverse_proxy is configured with
// `header_up X-Forwarded-For {remote_host}` (controls what the header can
// say — it discards any client-supplied value and replaces it with the real
// peer address, rather than merely trusting Caddy's default behavior with no
// trusted_proxies set). If a future change fronts this with another proxy
// (CloudFront, an ALB) via Caddy's `trusted_proxies`, that second condition
// can silently stop holding and this becomes forgeable again.
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
