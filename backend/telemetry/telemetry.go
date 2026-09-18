package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)


// Init registers a real OTLP/gRPC-exporting TraceProvider and MeterProvider as the global providers and returns a shutdown func.
// Callers elsewhere use otel.Tracer(name)/otel.Meter(name) directly, not this package.
func Init(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("building resource: %w", err)
	}

	// WithInsecure(): both exporters default to TLS, which a local collector without a cert can't satisfy.
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("constructing OTLP trace exporter: %w", err)
	}
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	// Built before either provider is registered globally, so a failure here leaves nothing stranded in the global slot.
	metricExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithInsecure())
	if err != nil {
		_ = traceProvider.Shutdown(ctx)
		return nil, fmt.Errorf("constructing OTLP metric exporter: %w", err)
	}
	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter)),
		metric.WithResource(res),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(meterProvider)

	shutdown := func(ctx context.Context) error {
		var errs []error
		if err := traceProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutting down tracer provider: %w", err))
		}
		if err := meterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutting down metric provider: %w", err))
		}
		if len(errs) > 0 {
			return fmt.Errorf("telemetry shutdown errors: %v", errs)
		}
		return nil
	}
	return shutdown, nil
}
