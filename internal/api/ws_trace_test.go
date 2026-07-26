package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/joekhosbayar/go-mighty/internal/obs"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
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

// TestWSHandlerMessageSpanNameIsBoundedForGarbageType guards the cardinality
// hole finding #1 of the final review closed: inMsg.Type is unbounded
// attacker-controlled JSON (up to maxWSMessageBytes = 32KB), and WSHandler
// (ws.go) must sanitize it through wsMessageTypeLabel - the same sanitizer
// already applied on the RecordWSMessage/metric path - before it ever
// reaches the span NAME. Grafana Cloud Traces' span-metrics generator turns
// span_name into a metric series by default, so an unbounded span name
// reopens the exact free-tier cardinality exhaustion the sanitizer exists to
// prevent.
//
// This drives a real message through the real WSHandler (not a direct call
// to StartWSMessageSpanWithTracer) so a regression at the ws.go call site -
// e.g. reverting back to passing inMsg.Type straight through - actually
// fails this test. That requires installing a recording TracerProvider as
// the process-global one, since obs.Tracer() (which WSHandler uses) reads
// otel.GetTracerProvider(); this test is deliberately NOT t.Parallel() so it
// never overlaps with another test recording spans through that same global.
//
//nolint:paralleltest // deliberately not parallel: mutates the process-global TracerProvider that obs.Tracer() reads.
func TestWSHandlerMessageSpanNameIsBoundedForGarbageType(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	prevTP := otel.GetTracerProvider()

	otel.SetTracerProvider(tp)

	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)

		_ = tp.Shutdown(context.Background())
	})

	server, _ := setupWSTestServer(t)
	conn := dialWS(t, server, "/games/game-1/ws", generateValidToken("user-1", "alice"))

	// Comfortably under maxWSMessageBytes once JSON-wrapped, but far larger
	// than any legitimate message type.
	garbage := strings.Repeat("A", 30<<10)
	if err := conn.WriteJSON(map[string]any{keyType: garbage}); err != nil {
		t.Fatalf("failed to write garbage-typed message: %v", err)
	}

	// The handshake span ends first; poll for a "ws.message " span too
	// instead of a fixed sleep, since a non-MOVE frame doesn't close the
	// connection or signal WaitForProcessMove.
	deadline := time.Now().Add(2 * time.Second)

	var msgSpan sdktrace.ReadOnlySpan
	for time.Now().Before(deadline) && msgSpan == nil {
		for _, s := range rec.Ended() {
			if strings.HasPrefix(s.Name(), "ws.message ") {
				msgSpan = s
				break
			}
		}

		if msgSpan == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}

	require.NotNilf(t, msgSpan, "expected a ws.message span to be recorded")

	name := msgSpan.Name()
	require.Lessf(t, len(name), 64, "span name must be bounded, got %d bytes: %.64q", len(name), name)
	require.Equal(t, "ws.message "+obs.MsgTypeUnknown, name)
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
