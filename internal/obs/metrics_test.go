package obs

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// collect gathers all metrics recorded through a manual reader.
func collect(t *testing.T, fn func(*Metrics)) metricdata.ResourceMetrics {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	m, err := NewMetrics(mp)
	require.NoError(t, err)

	fn(m)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	return rm
}

// findSum returns the data points of a named Int64 counter.
func findSum(t *testing.T, rm metricdata.ResourceMetrics, name string) []metricdata.DataPoint[int64] {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}

			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "metric %s is not an int64 sum", name)

			return sum.DataPoints
		}
	}

	t.Fatalf("metric %s not recorded", name)

	return nil
}

func TestRecordHandshakeCarriesOutcomeAndKind(t *testing.T) {
	t.Parallel()

	rm := collect(t, func(m *Metrics) {
		m.RecordHandshake(context.Background(), KindGame, OutcomeOK)
		m.RecordHandshake(context.Background(), KindGame, OutcomeOK)
		m.RecordHandshake(context.Background(), KindLobby, OutcomeOriginRejected)
	})

	points := findSum(t, rm, "mighty.ws.handshake")
	require.Len(t, points, 2, "expected one series per (kind, outcome) pair")

	byOutcome := map[string]int64{}

	for _, p := range points {
		outcome, ok := p.Attributes.Value("outcome")
		require.True(t, ok)

		byOutcome[outcome.AsString()] = p.Value
	}

	require.Equal(t, int64(2), byOutcome[OutcomeOK])
	require.Equal(t, int64(1), byOutcome[OutcomeOriginRejected])
}

func TestAddConnectionTracksActiveCount(t *testing.T) {
	t.Parallel()

	rm := collect(t, func(m *Metrics) {
		m.AddConnection(context.Background(), KindGame, 1)
		m.AddConnection(context.Background(), KindGame, 1)
		m.AddConnection(context.Background(), KindGame, -1)
	})

	points := findSum(t, rm, "mighty.ws.connections.active")
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[0].Value)
}

func TestRecordMoveEmitsCounterAndHistogram(t *testing.T) {
	t.Parallel()

	rm := collect(t, func(m *Metrics) {
		m.RecordMove(context.Background(), MoveValid, 0.012)
	})

	require.Len(t, findSum(t, rm, "mighty.moves"), 1)

	var found bool

	for _, sm := range rm.ScopeMetrics {
		for _, mm := range sm.Metrics {
			if mm.Name == "mighty.move.duration" {
				found = true
			}
		}
	}

	require.True(t, found, "move duration histogram not recorded")
}

// Every recorder must tolerate a nil receiver: existing tests construct
// api.NewHandler with no options, so the metrics pointer is legitimately nil.
func TestNilMetricsIsSafe(t *testing.T) {
	t.Parallel()

	var m *Metrics

	ctx := context.Background()

	require.NotPanics(t, func() {
		m.RecordHandshake(ctx, KindGame, OutcomeOK)
		m.AddConnection(ctx, KindGame, 1)
		m.RecordWSMessage(ctx, "MOVE", MsgAccepted)
		m.RecordRateLimitRejection(ctx, "creategame")
		m.RecordGameCreated(ctx)
		m.RecordMove(ctx, MoveValid, 0.1)
		m.RecordOptimisticConflict(ctx)
	})
}

// The cardinality rule from the spec, enforced rather than commented.
// game_id / user_id / IP on a metric label is unbounded and would consume the
// 10k free-tier series budget, after which data is silently dropped.
func TestNoForbiddenMetricLabels(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile("metrics.go")
	require.NoError(t, err)

	for _, forbidden := range []string{
		`"game_id"`, `"user_id"`, `"ip"`, `"client_ip"`, `"remote"`, `"conn_id"`,
	} {
		require.NotContains(t, string(src), forbidden,
			"%s must never be a metric label - it is unbounded. Put it on a span or a log field instead.", forbidden)
	}
}
