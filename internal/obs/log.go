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
//
// Returns *zerolog.Logger, not a value: zerolog.Logger's level methods
// (Info/Warn/Error/...) have pointer receivers, and a value returned from a
// function call is not addressable, so callers could not chain
// Log(ctx).Info() without this. In the no-span case this returns
// &zlog.Logger — the address of the package-level global, not a copy — which
// is deliberate: it keeps that path allocation-free and means a later
// reassignment of the global (e.g. in tests that swap it) is observed by
// anyone holding a previously-returned pointer.
func Log(ctx context.Context) *zerolog.Logger {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return &zlog.Logger
	}

	l := zlog.Logger.With().
		Str("trace_id", sc.TraceID().String()).
		Str("span_id", sc.SpanID().String()).
		Logger()

	return &l
}
