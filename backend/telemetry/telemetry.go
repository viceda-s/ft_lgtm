package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	otelpyroscope "github.com/grafana/otel-profiling-go"
	"github.com/grafana/pyroscope-go"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	logglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const metricExportInterval = 5 * time.Second

func Init(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("building resource: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("constructing OTLP trace exporter: %w", err)
	}
	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

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

	var profiler *pyroscope.Profiler
	if addr := os.Getenv("PYROSCOPE_SERVER_ADDRESS"); addr != "" {
		profiler, err = pyroscope.Start(pyroscope.Config{
			ApplicationName: serviceName,
			ServerAddress:   addr,
			ProfileTypes: []pyroscope.ProfileType{
				pyroscope.ProfileCPU,
				pyroscope.ProfileAllocObjects,
				pyroscope.ProfileAllocSpace,
				pyroscope.ProfileInuseObjects,
				pyroscope.ProfileInuseSpace,
				pyroscope.ProfileGoroutines,
			},
		})
		if err != nil {
			_ = traceProvider.Shutdown(ctx)
			_ = meterProvider.Shutdown(ctx)
			_ = loggerProvider.Shutdown(ctx)
			return nil, fmt.Errorf("starting pyroscope profiler: %w", err)
		}
	}

	otel.SetTracerProvider(otelpyroscope.NewTracerProvider(traceProvider,
		otelpyroscope.WithSpanIDLabelScope(otelpyroscope.ScopeAllSpans),
		otelpyroscope.WithSpanNameLabelScope(otelpyroscope.ScopeAllSpans)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetMeterProvider(meterProvider)
	logglobal.SetLoggerProvider(loggerProvider)

	shutdown := func(ctx context.Context) error {
		var errs []error
		if profiler != nil {
			if err := profiler.Stop(); err != nil {
				errs = append(errs, fmt.Errorf("stopping profiler: %w", err))
			}
		}
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

func Logger(name string) *slog.Logger {
	return otelslog.NewLogger(name, otelslog.WithLoggerProvider(logglobal.GetLoggerProvider()))
}
