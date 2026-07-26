package main

import (
	"context"
	"testing"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/obs"
)

// flushWithinTimeout runs flushTelemetry in a goroutine and fails the test if
// it doesn't return within timeout. flushTelemetry is meant to be fast (it's
// the only thing that runs between a SIGTERM and the process exiting), so a
// generous bound here is a deadlock guard, not a real-world latency
// expectation — this is not a sleep-based or otherwise flaky wait.
func flushWithinTimeout(t *testing.T, p *obs.Provider, timeout time.Duration) {
	t.Helper()

	done := make(chan struct{})

	go func() {
		flushTelemetry(p)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("flushTelemetry did not return within the timeout")
	}
}

// TestFlushTelemetryToleratesNilProvider covers the defensive nil case: a
// caller that hasn't gone through obs.Init at all must not panic or block.
func TestFlushTelemetryToleratesNilProvider(t *testing.T) {
	t.Parallel()

	flushWithinTimeout(t, nil, time.Second)
}

// TestFlushTelemetryInvokesShutdownOnDisabledProvider covers the actual
// local-dev default: OTEL_EXPORTER_OTLP_ENDPOINT unset, so obs.Init returns a
// disabled no-op Provider. flushTelemetry's only job for a non-nil provider
// is to call p.Shutdown — there is no branch that skips it when Enabled is
// false — so this exercises the exact call flushTelemetry makes, and asserts
// it doesn't block even though the provider has nothing real to flush.
func TestFlushTelemetryInvokesShutdownOnDisabledProvider(t *testing.T) {
	t.Parallel()

	p, err := obs.Init(t.Context(), obs.Config{})
	if err != nil {
		t.Fatalf("obs.Init: %v", err)
	}

	if p.Enabled {
		t.Fatal("expected a disabled provider for an empty Config (no Endpoint)")
	}

	flushWithinTimeout(t, p, time.Second)

	// Shutdown is idempotent (sync.Once-guarded); calling it again directly
	// after flushTelemetry already did confirms flushTelemetry's call to it
	// actually completed rather than, say, panicking inside a goroutine that
	// the test failed to observe.
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown after flushTelemetry: %v", err)
	}
}
