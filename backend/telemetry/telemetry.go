package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	logglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// metricExportInterval controls how often the PeriodicReader flushes metrics to the collector. The SDK's default is 60s, which made the Grafana dashboards (refreshing every 10s) look stalled for up to a minute after a code execution. A short interval keeps the dashboards close to real-time.
const metricExportInterval = 5 * time.Second

// Init registers a real OTLP/gRPC-exporting TraceProvider and MeterProvider as the global providers and returns a shutdown func.
// Callers elsewhere use otel.Tracer(name)/otel.Meter(name) directly, not this package.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is set explicitly, it must include a scheme (e.g. "http://otel-collector:4317") — without one, the SDK's URL parser silently resolves an empty address and exports fail later with "missing address".
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
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(metricExportInterval))),
		metric.WithResource(res),
	)

	logExporter, err := otlploggrpc.New(ctx, otlploggrpc.WithInsecure())
	if err != nil {
		_ = traceProvider.Shutdown(ctx)
		_ = meterProvider.Shutdown(ctx)
		return nil, fmt.Errorf("constructing OTLP log exporter: %w", err)
	}
	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetMeterProvider(meterProvider)
	logglobal.SetLoggerProvider(loggerProvider)

	shutdown := func(ctx context.Context) error {
		var errs []error
		if err := traceProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutting down tracer provider: %w", err))
		}
		if err := meterProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutting down metric provider: %w", err))
		}
		if err := loggerProvider.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutting down logger provider: %w", err))
		}
		if len(errs) > 0 {
			return fmt.Errorf("telemetry shutdown errors: %v", errs)
		}
		return nil
	}
	return shutdown, nil
}

// Logger returns a *slog.Logger backed by the globally registered OTel LoggerProvider. Log records emitted via its *Context methods automatically carry the active span's trace_id/span_id when ctx holds one. Before Init runs, OTel's own no-op global LoggerProvider is used, so calling Logger before Init is always safe (same behavior as otel.Tracer(...)/otel.Meter(...)).
func Logger(name string) *slog.Logger {
	return otelslog.NewLogger(name, otelslog.WithLoggerProvider(logglobal.GetLoggerProvider()))
}
