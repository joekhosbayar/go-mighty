// Package obs owns this service's OpenTelemetry wiring: SDK lifecycle, metric
// instruments, and the bridge between zerolog and the active span.
//
// Everything here is inert unless OTEL_EXPORTER_OTLP_ENDPOINT is set. That is
// deliberate: the dev compose stack has no collector and `go test ./...` must
// not open sockets or spawn exporter goroutines.
package obs

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// ServiceName is the resource name every signal is tagged with.
const ServiceName = "mighty"

// exportInterval matches the Alloy scrape interval. Pushing more often than
// the backend stores costs bandwidth on a small box and buys nothing.
const exportInterval = 60 * time.Second

// exporterTimeout bounds a single export attempt. Kept short (rather than the
// OTel default retry policy's up-to-a-minute backoff) so a stalled or absent
// collector never turns process shutdown into a multi-second hang.
const exporterTimeout = 5 * time.Second

// Config describes the SDK setup. An empty Endpoint disables everything.
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	// Endpoint is an OTLP gRPC address such as "alloy:4317". Empty disables
	// all instrumentation.
	Endpoint string
	// SampleRatio is the parent-based trace sampling ratio, 0..1.
	SampleRatio float64
}

// ConfigFromEnv reads the standard OTEL_* variables. A malformed sample ratio
// falls back to 1 rather than 0: silently sampling nothing looks identical to
// a working system right up until you need a trace.
func ConfigFromEnv() Config {
	cfg := Config{
		ServiceName:    ServiceName,
		ServiceVersion: os.Getenv("MIGHTY_VERSION"),
		Environment:    os.Getenv("MIGHTY_ENV"),
		Endpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		SampleRatio:    1,
	}

	if raw := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 && v <= 1 {
			cfg.SampleRatio = v
		}
	}

	return cfg
}

// Provider holds the configured providers and their shutdown hooks.
type Provider struct {
	// Enabled reports whether real exporters were installed. Callers use it
	// to decide whether to log that telemetry is active.
	Enabled bool
	Tracer  trace.TracerProvider
	Meter   metric.MeterProvider

	shutdownOnce sync.Once
	shutdownFns  []func(context.Context) error
	shutdownErr  error
}

// Init builds the providers and installs them globally. With cfg.Endpoint
// empty it installs no-op providers and returns a Provider whose Shutdown is
// a no-op, so callers need no conditional wiring.
func Init(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = ServiceName
	}

	if cfg.Endpoint == "" {
		p := &Provider{
			Enabled: false,
			Tracer:  tracenoop.NewTracerProvider(),
			Meter:   metricnoop.NewMeterProvider(),
		}
		otel.SetTracerProvider(p.Tracer)
		otel.SetMeterProvider(p.Meter)

		return p, nil
	}

	attrs := []attribute.KeyValue{semconv.ServiceName(cfg.ServiceName)}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
	}

	if cfg.Environment != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentNameKey.String(cfg.Environment))
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(semconv.SchemaURL, attrs...))
	if err != nil {
		return nil, err
	}

	// WithInsecure: the collector is a sibling container on a private compose
	// network. TLS here would protect a hop that never leaves the host, and
	// Alloy is the component that speaks TLS to Grafana Cloud.
	//
	// WithRetry disabled + a short WithTimeout: the default retry policy
	// backs off for up to a minute per export attempt, which turns
	// Shutdown into a multi-second (or longer) stall whenever the collector
	// is briefly unreachable. A single fast-failing attempt is the right
	// trade-off for a batched, best-effort signal.
	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithTimeout(exporterTimeout),
		otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}))
	if err != nil {
		return nil, err
	}

	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithTimeout(exporterTimeout),
		otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{Enabled: false}))
	if err != nil {
		return nil, errors.Join(err, traceExp.Shutdown(ctx))
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(exportInterval))),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return &Provider{
		Enabled:     true,
		Tracer:      tp,
		Meter:       mp,
		shutdownFns: []func(context.Context) error{tp.Shutdown, mp.Shutdown},
	}, nil
}

// Shutdown flushes and stops the exporters. Safe to call more than once.
func (p *Provider) Shutdown(ctx context.Context) error {
	p.shutdownOnce.Do(func() {
		var errs []error
		for _, fn := range p.shutdownFns {
			errs = append(errs, fn(ctx))
		}

		p.shutdownErr = errors.Join(errs...)
	})

	return p.shutdownErr
}
