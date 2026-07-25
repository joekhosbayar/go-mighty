package obs

import (
	"context"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

// Log returns the global logger with trace_id and span_id bound when ctx
// carries a valid span, and unchanged otherwise.
//
// This is an explicit helper rather than a zerolog.Hook because zerolog hooks
// receive no context: a hook could only read a ctx that call sites had already
// attached, which is the same call-site change with more indirection.
//
// trace_id is a log FIELD, never a Loki label — it is unbounded and would
// create one log stream per trace.
func Log(ctx context.Context) zerolog.Logger {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return zlog.Logger
	}

	return zlog.Logger.With().
		Str("trace_id", sc.TraceID().String()).
		Str("span_id", sc.SpanID().String()).
		Logger()
}
