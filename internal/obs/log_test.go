package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// captureLog swaps the global zerolog logger for one writing to a buffer and
// returns the decoded single line that was written.
func captureLog(t *testing.T, fn func()) map[string]any {
	t.Helper()

	var buf bytes.Buffer

	original := zlog.Logger
	zlog.Logger = zerolog.New(&buf)

	t.Cleanup(func() { zlog.Logger = original })

	fn()

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))

	return out
}

//nolint:paralleltest // captureLog swaps the shared zlog.Logger package-level global; running concurrently with the other test in this file that does the same would race.
func TestLogAddsTraceIDWhenSpanPresent(t *testing.T) {
	tp := sdktrace.NewTracerProvider()

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	out := captureLog(t, func() {
		logger := Log(ctx)
		logger.Info().Msg("hello")
	})

	require.Equal(t, span.SpanContext().TraceID().String(), out["trace_id"])
	require.Equal(t, span.SpanContext().SpanID().String(), out["span_id"])
}

//nolint:paralleltest // see TestLogAddsTraceIDWhenSpanPresent.
func TestLogOmitsTraceIDWithoutSpan(t *testing.T) {
	out := captureLog(t, func() {
		logger := Log(context.Background())
		logger.Info().Msg("hello")
	})

	require.NotContains(t, out, "trace_id")
	require.NotContains(t, out, "span_id")
}
