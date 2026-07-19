package api

import "net/http"

// HealthzHandler is a shallow liveness probe for Route 53 health checks and
// load-path smoke tests. It deliberately checks nothing downstream: a dying
// dependency should page via its own alarms, not flap DNS.
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
