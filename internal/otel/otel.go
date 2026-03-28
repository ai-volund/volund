// Package otel bootstraps OpenTelemetry tracing and metrics for Volund services.
package otel

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Config holds OTel bootstrap configuration.
type Config struct {
	ServiceName  string // e.g. "volund-gateway", "volund-agent"
	OTLPEndpoint string // gRPC endpoint for OTLP collector (e.g. "localhost:4317")
	Environment  string // "dev", "staging", "prod"
}

// Shutdown is returned by Init and must be called on process exit.
type Shutdown func(ctx context.Context) error

// Init sets up the global TracerProvider, MeterProvider, and propagators.
// Returns a Shutdown function that flushes and closes exporters.
// If otlpEndpoint is empty, a no-op tracer is used (metrics still work via Prometheus).
func Init(cfg Config) (Shutdown, error) {
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcess(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: build resource: %w", err)
	}

	var shutdowns []func(context.Context) error

	// --- Traces ---
	var tp *sdktrace.TracerProvider
	if cfg.OTLPEndpoint != "" {
		exporter, err := otlptracegrpc.New(context.Background(),
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, fmt.Errorf("otel: create trace exporter: %w", err)
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(res),
		)
		shutdowns = append(shutdowns, tp.Shutdown)
	} else {
		// No collector configured — use a passthrough provider that still
		// generates trace/span IDs (useful for log correlation).
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
		)
		shutdowns = append(shutdowns, tp.Shutdown)
	}
	otel.SetTracerProvider(tp)

	// --- Propagation ---
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// --- Metrics (Prometheus) ---
	promExporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("otel: create prometheus exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(promExporter),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	shutdowns = append(shutdowns, mp.Shutdown)

	shutdown := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		for _, fn := range shutdowns {
			if err := fn(ctx); err != nil {
				return err
			}
		}
		return nil
	}

	return shutdown, nil
}

// Tracer returns a named tracer from the global provider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
