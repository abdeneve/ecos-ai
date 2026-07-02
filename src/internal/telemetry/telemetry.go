// Package telemetry provides the structured logging and tracing setup shared
// by both the ingress and worker binaries.
package telemetry

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// NewLogger returns a JSON structured logger tagged with the service name,
// so ingress and worker logs can be told apart when aggregated.
func NewLogger(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler).With("service", service)
}

// InitTracer wires a tracer provider for the given service name and
// registers it as the global provider. The returned shutdown function must
// be called on process exit to flush pending spans.
//
// The exporter is stdout-based by default (no collector dependency for
// local development); swap for an OTLP exporter when a collector is
// available without changing call sites, since callers only depend on the
// returned trace.Tracer.
func InitTracer(ctx context.Context, service string) (trace.Tracer, func(context.Context) error, error) {
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, nil, err
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(service)),
	)
	if err != nil {
		return nil, nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Tracer(service), tp.Shutdown, nil
}
