// Package tracing wires OpenTelemetry trace export.
package tracing

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/teo-garcia/gin-template-monolith/internal/config"
)

// Shutdown flushes and stops the trace pipeline.
type Shutdown func(context.Context) error

// Setup installs the global tracer provider and propagator.
//
// When tracing is disabled it returns a no-op shutdown rather than an error, so
// callers do not need a conditional at every call site.
func Setup(ctx context.Context, cfg config.Config) (Shutdown, error) {
	if !cfg.OTel.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	endpoint, insecure, err := parseEndpoint(cfg.OTel.Endpoint)
	if err != nil {
		return nil, err
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpoint.Host),
		otlptracehttp.WithURLPath(endpoint.Path),
		otlptracehttp.WithTimeout(10 * time.Second),
	}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewSchemaless(
			semconv.ServiceName(cfg.OTel.ServiceName),
			semconv.ServiceVersion(cfg.App.Version),
			attribute.String("deployment.environment", cfg.App.Env),
		)),
	)

	otel.SetTracerProvider(provider)
	// W3C trace context plus baggage is what lets a trace cross service
	// boundaries; without a propagator every service starts a new trace.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return provider.Shutdown, nil
}

func parseEndpoint(raw string) (*url.URL, bool, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, false, fmt.Errorf("parse OTEL endpoint %q: %w", raw, err)
	}
	if parsed.Host == "" {
		return nil, false, fmt.Errorf("OTEL endpoint %q has no host", raw)
	}
	return parsed, !strings.EqualFold(parsed.Scheme, "https"), nil
}
