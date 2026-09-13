// Package observability wires OpenTelemetry tracing and Sentry error
// reporting.
//
// Both are optional. With no endpoint or DSN configured they install
// no-op implementations, so development needs no collector and no Sentry
// project — but the instrumentation is present from the start, which
// means turning it on in staging is a configuration change rather than a
// code change.
//
// Relationship to internal/platform/logging: that package owns the
// trace ID that appears in log lines. This package makes that ID the
// OpenTelemetry trace ID when a span is active, so a log line and a span
// can be joined. Without that, you get two independent correlation
// schemes and neither is complete.
package observability

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Config is the subset of application config this package needs.
type Config struct {
	ServiceName  string
	Version      string
	Environment  string
	OTLPEndpoint string // empty disables export
	SampleRatio  float64
}

// Shutdown flushes any buffered telemetry. Always non-nil, so callers
// can defer it unconditionally.
type Shutdown func(context.Context) error

// Init installs the global tracer provider and propagator.
//
// The W3C propagator is installed even when export is disabled. That is
// deliberate: propagation is what carries the trace across the three
// services, and it costs nothing. Only the exporter is conditional.
func Init(ctx context.Context, cfg Config) (Shutdown, error) {
	// Propagation first, and unconditionally.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if cfg.OTLPEndpoint == "" {
		// No collector configured: a no-op provider. Spans are created
		// and discarded, so instrumentation code paths still execute and
		// cannot rot, but nothing is exported or buffered.
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(cfg.OTLPEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
		attribute.String("deployment.environment", cfg.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	ratio := cfg.SampleRatio
	if ratio <= 0 || ratio > 1 {
		ratio = 1
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		// ParentBased keeps a trace intact end to end: if api-service
		// sampled a request, the Python services must sample it too, or
		// the trace arrives with holes in it.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	otel.SetTracerProvider(tp)

	return func(ctx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return tp.Shutdown(shutdownCtx)
	}, nil
}

// TraceIDFromContext returns the active span's trace ID, or "" when
// there is no recording span.
//
// This is what lets the logging package prefer the OpenTelemetry trace
// ID over its own generated one, so a log line and a span share an
// identifier instead of carrying two unrelated ones.
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
