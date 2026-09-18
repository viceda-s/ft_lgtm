package telemetry

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)


// testOTLPEndpoint returns the value of OTEL_TEST_ENDPOINT (e.g. "127.0.0.1:4317" pointed at a real local otel-collector or a throwaway gRPC OTLP receiver), skipping the test if unset.
// Tests in this file that need a real network round-trip use this; tests that only need to assert span/metric structure use the in-memory recorders instead (Step 5+).
func testOTLPEndpoint(t *testing.T) string {
	t.Helper()
	endpoint := os.Getenv("OTEL_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("OTEL_TEST_ENDPOINT not set; skipping real-OTEL-endpoint test")
	}
	return endpoint
}


func TestInit_ValidEndpoint_ReturnsWorkingShutdown(t *testing.T) {
	endpoint := testOTLPEndpoint(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := Init(ctx, "ft_lgtm_test")
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected a non-nil shutdown func")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown returned error: %v", err)
	}
}


func TestInit_NoEndpointConfigured_UsesDefaultWithoutError(t *testing.T) {
	// No OTEL_EXPORTER_OTLP_ENDPOINT set: Init must still succeed (the SDK defaults to localhost:4317 and only fails at actual export time, not at construction time) — this test runs unconditionally, no real collector needed, since gRPC exporter construction doesn't dial eagerly.
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := Init(ctx, "ft_lgtm_test")
	if err != nil {
		t.Fatalf("Init returned error with no endpoint configured: %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = shutdown(shutdownCtx) // best-effort; no real collector to flush to
}


// unusedPort finds a TCP port nothing is listening on, for the unreachable endpoint test below.
func unusedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding an unused port: %v", err)
	}
	defer l.Close()
	return l.Addr().String()
}


func TestInit_UnreachableEndpoint_ConstructionStillSucceeds(t *testing.T) {
	// gRPC exporters connect lazily — Init must not block on or fail from an unreachable endpoint at construction time (only actual span/metric export attempts would time out later, which is the SDK's own documented behavior, not something this package adds).
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", unusedPort(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := Init(ctx, "ft_lgtm_test")
	if err != nil {
		t.Fatalf("Init returned error for an unreachable (but syntactically valid) endpoint: %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	_ = shutdown(shutdownCtx)
}


func TestSpanRecording_ChildSpanWithAttribute_IsCaptured(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer("test")

	ctx, rootSpan := tracer.Start(context.Background(), "root")
	_, childSpan := tracer.Start(ctx, "child")
	childSpan.SetAttributes(attribute.String("code.hash", "abc123"))
	childSpan.End()
	rootSpan.End()

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("expected 2 ended spans, got %d", len(spans))
	}
	var root, child sdktrace.ReadOnlySpan
	for _, s := range spans {
		switch s.Name() {
		case "root":
			root = s
		case "child":
			child = s
		}
	}
	if root == nil {
		t.Fatalf("expected a span named \"root\"")
	}
	if child == nil {
		t.Fatalf("expected a span named \"child\"")
	}

	found := false
	for _, attr := range child.Attributes() {
		if attr.Key == "code.hash" && attr.Value.AsString() == "abc123" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected child span to carry code.hash=abc123 atrribute")
	}
	if child.Parent().SpanID() != root.SpanContext().SpanID() {
		t.Fatalf("expected child span's parent to be root span %s, got %s", root.SpanContext().SpanID(), child.Parent().SpanID())
	}
}