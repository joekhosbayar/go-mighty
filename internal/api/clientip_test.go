package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPUsesRemoteAddrWhenProxyIsUntrusted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games", nil)
	req.RemoteAddr = "10.0.0.5:41234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ClientIP(req, false); got != "10.0.0.5" {
		t.Fatalf("expected 10.0.0.5, got %q", got)
	}
}

func TestClientIPUsesForwardedForWhenProxyIsTrusted(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games", nil)
	req.RemoteAddr = "172.18.0.2:41234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 172.18.0.2")

	if got := ClientIP(req, true); got != "1.2.3.4" {
		t.Fatalf("expected 1.2.3.4, got %q", got)
	}
}

func TestClientIPFallsBackWhenForwardedForIsAbsent(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/games", nil)
	req.RemoteAddr = "172.18.0.2:41234"

	if got := ClientIP(req, true); got != "172.18.0.2" {
		t.Fatalf("expected 172.18.0.2, got %q", got)
	}
}
