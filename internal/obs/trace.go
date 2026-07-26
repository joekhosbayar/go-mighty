package obs

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/joekhosbayar/go-mighty"

// Tracer returns the service tracer from the globally installed provider,
// which is a no-op provider unless Init enabled exporting.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// StartWSMessageSpan starts a span for one inbound WebSocket frame.
//
// This is deliberately a NEW ROOT span per message rather than a child of a
// connection-scoped span: a connection span would stay open for an entire
// game, which is unusable in Tempo and holds SDK memory for hours.
//
// game_id, user_id and conn_id are span attributes. They must never become
// metric labels - see the cardinality guard in metrics_test.go.
func StartWSMessageSpan(ctx context.Context, msgType, gameID, userID, connID string) (context.Context, trace.Span) {
	return StartWSMessageSpanWithTracer(ctx, Tracer(), msgType, gameID, userID, connID)
}

// StartWSMessageSpanWithTracer is StartWSMessageSpan with an explicit tracer,
// so tests can record spans without touching global state.
func StartWSMessageSpanWithTracer(
	ctx context.Context,
	tracer trace.Tracer,
	msgType, gameID, userID, connID string,
) (context.Context, trace.Span) {
	return tracer.Start(ctx, "ws.message "+msgType,
		trace.WithNewRoot(),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("game_id", gameID),
			attribute.String("user_id", userID),
			attribute.String("conn_id", connID),
			attribute.String("ws.message_type", msgType),
		))
}

// StartWSHandshakeSpan starts a short-lived span covering upgrade, AUTH, and
// the accept/reject decision — the part of a WebSocket's life that actually
// fails. It deliberately does NOT span the connection: see
// StartWSMessageSpan for why.
//
// The caller must call EndWSHandshakeSpan on every exit path.
func StartWSHandshakeSpan(ctx context.Context, kind string) (context.Context, trace.Span) {
	return Tracer().Start(ctx, "ws.handshake "+kind,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("kind", kind)))
}

// EndWSHandshakeSpan records the handshake outcome and closes the span. Any
// outcome other than OutcomeOK is marked as an error status so handshake
// failures surface in Tempo's error view without a query.
func EndWSHandshakeSpan(span trace.Span, outcome string) {
	span.SetAttributes(attribute.String("outcome", outcome))

	if outcome != OutcomeOK {
		span.SetStatus(codes.Error, outcome)
	}

	span.End()
}
