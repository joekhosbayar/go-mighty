package api

import (
	"context"
	"testing"

	"github.com/joekhosbayar/go-mighty/internal/obs"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A per-message span, not a per-connection one: a connection span would stay
// open for an entire game, which is useless in Tempo and a leak in the SDK.
// game_id and user_id belong here, on the span - never on a metric label.
func TestWSMessageSpanCarriesIdentifiers(t *testing.T) {
	t.Parallel()

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	ctx, span := obs.StartWSMessageSpanWithTracer(context.Background(),
		tp.Tracer("test"), "MOVE", "game-1", "user-1", "conn-1")
	require.NotNil(t, ctx)
	span.End()

	ended := rec.Ended()
	require.Len(t, ended, 1)
	require.Equal(t, "ws.message MOVE", ended[0].Name())

	attrs := map[string]string{}
	for _, kv := range ended[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}

	require.Equal(t, "game-1", attrs["game_id"])
	require.Equal(t, "user-1", attrs["user_id"])
	require.Equal(t, "conn-1", attrs["conn_id"])
}

// The handshake span must close with an error status on every rejection path,
// so handshake failures show up in Tempo's error view without a query.
func TestWSHandshakeSpanRecordsOutcome(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{
		obs.OutcomeOK,
		obs.OutcomeAuthTimeout,
		obs.OutcomeAuthFailed,
		obs.OutcomeAuthUnavailable,
		obs.OutcomeOriginRejected,
		obs.OutcomeUpgradeFailed,
		obs.OutcomeConnLimitUser,
		obs.OutcomeConnLimitIP,
	} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()

			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

			_, span := tp.Tracer("test").Start(context.Background(), "ws.handshake game")
			obs.EndWSHandshakeSpan(span, outcome)

			ended := rec.Ended()
			require.Len(t, ended, 1)

			attrs := map[string]string{}
			for _, kv := range ended[0].Attributes() {
				attrs[string(kv.Key)] = kv.Value.AsString()
			}

			require.Equal(t, outcome, attrs["outcome"])

			if outcome == obs.OutcomeOK {
				require.Equal(t, codes.Unset, ended[0].Status().Code)
			} else {
				require.Equal(t, codes.Error, ended[0].Status().Code)
			}
		})
	}
}
