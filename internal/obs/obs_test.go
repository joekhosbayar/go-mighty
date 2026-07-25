package obs

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// An unset endpoint is the dev and test default: no exporter, no network, no
// goroutines. goleak in TestMain is what actually enforces the last part.
func TestInitDisabledWhenEndpointEmpty(t *testing.T) {
	t.Parallel()

	p, err := Init(context.Background(), Config{ServiceName: "test"})
	require.NoError(t, err)
	require.False(t, p.Enabled)

	// Not SDK providers — no batching goroutines, no export attempts.
	require.NotImplements(t, (*interface{ ForceFlush(context.Context) error })(nil), p.Tracer)

	require.NoError(t, p.Shutdown(context.Background()))
}

// The gRPC exporters connect lazily, so Init succeeds with no collector
// listening — which is what makes it testable without a fixture. Shutdown is
// a different story: the metric PeriodicReader always attempts one final
// export, even with zero recorded data points, so against an unreachable
// endpoint it genuinely fails fast (WithRetry disabled, a short WithTimeout)
// rather than hanging. The trace BatchSpanProcessor has no pending spans to
// flush and so shuts down cleanly. errors.Join surfaces the metric side.
func TestInitEnabledBuildsSDKProviders(t *testing.T) {
	t.Parallel()

	p, err := Init(context.Background(), Config{
		ServiceName: "test",
		Endpoint:    "localhost:4317",
		SampleRatio: 1,
	})
	require.NoError(t, err)
	require.True(t, p.Enabled)
	require.IsType(t, &sdktrace.TracerProvider{}, p.Tracer)
	require.IsType(t, &sdkmetric.MeterProvider{}, p.Meter)

	err = p.Shutdown(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "connection refused")
}

// Shutdown must be safe to call more than once and must not re-attempt the
// export on the second call: sync.Once caches the first result, so both
// calls return the identical error instead of dialing twice.
func TestShutdownIsIdempotent(t *testing.T) {
	t.Parallel()

	p, err := Init(context.Background(), Config{ServiceName: "test", Endpoint: "localhost:4317", SampleRatio: 1})
	require.NoError(t, err)

	first := p.Shutdown(context.Background())
	require.Error(t, first)

	second := p.Shutdown(context.Background())
	require.Equal(t, first, second)
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "")

	cfg := ConfigFromEnv()
	require.Empty(t, cfg.Endpoint)
	require.InDelta(t, 1.0, cfg.SampleRatio, 0.001)
	require.Equal(t, "mighty", cfg.ServiceName)
}

func TestConfigFromEnvReadsSampleRatio(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "alloy:4317")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")

	cfg := ConfigFromEnv()
	require.Equal(t, "alloy:4317", cfg.Endpoint)
	require.InDelta(t, 0.25, cfg.SampleRatio, 0.001)
}

// A malformed ratio must not disable tracing silently — it falls back to 1.
func TestConfigFromEnvRejectsBadSampleRatio(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "alloy:4317")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "banana")

	cfg := ConfigFromEnv()
	require.InDelta(t, 1.0, cfg.SampleRatio, 0.001)
}
