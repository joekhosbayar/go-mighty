package obs

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Bounded label values. Every attribute in this file must come from a fixed
// set: this is the only file allowed to name metric attributes, and the
// cardinality guard test in metrics_test.go scans it for unbounded keys.
const (
	KindGame  = "game"
	KindLobby = "lobby"

	OutcomeOK              = "ok"
	OutcomeAuthTimeout     = "auth_timeout"
	OutcomeAuthFailed      = "auth_failed"
	OutcomeAuthUnavailable = "auth_unavailable"
	OutcomeOriginRejected  = "origin_rejected"
	OutcomeUpgradeFailed   = "upgrade_failed"
	OutcomeConnLimitUser   = "conn_limit_user"
	OutcomeConnLimitIP     = "conn_limit_ip"

	MsgAccepted    = "accepted"
	MsgRateLimited = "rate_limited"
	MsgInvalid     = "invalid"

	// MsgTypeUnknown labels a frame rejected before its type could be read.
	MsgTypeUnknown = "unknown"

	MoveValid    = "valid"
	MoveInvalid  = "invalid"
	MoveConflict = "conflict"
	MoveError    = "error"
)

// Metrics holds every application instrument. A nil *Metrics is valid and
// every method is a no-op on it, so callers that were constructed without
// telemetry (local dev, the existing test suite) need no branching.
type Metrics struct {
	wsHandshake         metric.Int64Counter
	wsConnectionsActive metric.Int64UpDownCounter
	wsMessages          metric.Int64Counter
	rateLimitRejections metric.Int64Counter
	gamesCreated        metric.Int64Counter
	moves               metric.Int64Counter
	moveDuration        metric.Float64Histogram
	optimisticConflicts metric.Int64Counter
}

// NewMetrics registers the instruments on mp.
func NewMetrics(mp metric.MeterProvider) (*Metrics, error) {
	meter := mp.Meter("github.com/joekhosbayar/go-mighty/internal/obs")

	var (
		m   Metrics
		err error
	)

	if m.wsHandshake, err = meter.Int64Counter("mighty.ws.handshake",
		metric.WithDescription("WebSocket handshake attempts by outcome"),
	); err != nil {
		return nil, err
	}

	if m.wsConnectionsActive, err = meter.Int64UpDownCounter("mighty.ws.connections.active",
		metric.WithDescription("Currently open WebSocket connections"),
	); err != nil {
		return nil, err
	}

	if m.wsMessages, err = meter.Int64Counter("mighty.ws.messages",
		metric.WithDescription("Inbound WebSocket frames by type and transport-level outcome"),
	); err != nil {
		return nil, err
	}

	if m.rateLimitRejections, err = meter.Int64Counter("mighty.ratelimit.rejections",
		metric.WithDescription("HTTP requests rejected by the per-user rate limiter"),
	); err != nil {
		return nil, err
	}

	if m.gamesCreated, err = meter.Int64Counter("mighty.games.created",
		metric.WithDescription("Games created"),
	); err != nil {
		return nil, err
	}

	if m.moves, err = meter.Int64Counter("mighty.moves",
		metric.WithDescription("Moves processed by engine-level outcome"),
	); err != nil {
		return nil, err
	}

	if m.moveDuration, err = meter.Float64Histogram("mighty.move.duration",
		metric.WithDescription("ProcessMove latency"),
		metric.WithUnit("s"),
	); err != nil {
		return nil, err
	}

	if m.optimisticConflicts, err = meter.Int64Counter("mighty.optimistic_conflicts",
		metric.WithDescription("Version conflicts rejected by optimistic concurrency control"),
	); err != nil {
		return nil, err
	}

	return &m, nil
}

// RecordHandshake counts one WebSocket handshake attempt.
func (m *Metrics) RecordHandshake(ctx context.Context, kind, outcome string) {
	if m == nil {
		return
	}

	m.wsHandshake.Add(ctx, 1, metric.WithAttributes(
		attribute.String("kind", kind),
		attribute.String("outcome", outcome),
	))
}

// AddConnection adjusts the live connection gauge; pass -1 on close.
func (m *Metrics) AddConnection(ctx context.Context, kind string, delta int64) {
	if m == nil {
		return
	}

	m.wsConnectionsActive.Add(ctx, delta, metric.WithAttributes(attribute.String("kind", kind)))
}

// RecordWSMessage counts one inbound frame at the transport level. Engine
// verdicts on a move belong on RecordMove instead.
func (m *Metrics) RecordWSMessage(ctx context.Context, msgType, outcome string) {
	if m == nil {
		return
	}

	m.wsMessages.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", msgType),
		attribute.String("outcome", outcome),
	))
}

// RecordRateLimitRejection counts one per-user rate-limit rejection. bucket is
// the action name, which is a compile-time constant at every call site.
func (m *Metrics) RecordRateLimitRejection(ctx context.Context, bucket string) {
	if m == nil {
		return
	}

	m.rateLimitRejections.Add(ctx, 1, metric.WithAttributes(attribute.String("bucket", bucket)))
}

// RecordGameCreated counts one created game.
func (m *Metrics) RecordGameCreated(ctx context.Context) {
	if m == nil {
		return
	}

	m.gamesCreated.Add(ctx, 1)
}

// RecordMove counts one processed move and records its latency in seconds.
func (m *Metrics) RecordMove(ctx context.Context, outcome string, seconds float64) {
	if m == nil {
		return
	}

	attrs := metric.WithAttributes(attribute.String("outcome", outcome))
	m.moves.Add(ctx, 1, attrs)
	m.moveDuration.Record(ctx, seconds, attrs)
}

// RecordOptimisticConflict counts one rejected stale-version write.
func (m *Metrics) RecordOptimisticConflict(ctx context.Context) {
	if m == nil {
		return
	}

	m.optimisticConflicts.Add(ctx, 1)
}
