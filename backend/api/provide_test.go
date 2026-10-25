package api

import (
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return recorder
}

func waitForSpan(t *testing.T, recorder *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range recorder.Ended() {
			if s.Name() == name {
				return s
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("span %q never ended", name)
	return nil
}

func TestHandler_ValidCode_AnnouncesCIDInLinkedProvideTrace(t *testing.T) {
	recorder := recordSpans(t)
	uploader := &fakeUploader{cid: "bafyFAKECID", provided: make(chan string, 1)}

	executeWith(t, uploader)

	select {
	case cid := <-uploader.provided:
		if cid != "bafyFAKECID" {
			t.Fatalf("expected Provide for bafyFAKECID, got %q", cid)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Provide was never called")
	}
	provide := waitForSpan(t, recorder, "ipfs_provide")
	root := waitForSpan(t, recorder, "/api/execute")
	if provide.Parent().IsValid() || provide.SpanContext().TraceID() == root.SpanContext().TraceID() {
		t.Fatal("expected ipfs_provide to start its own trace so it does not stretch the execution trace")
	}
	if links := provide.Links(); len(links) != 1 || links[0].SpanContext.SpanID() != root.SpanContext().SpanID() {
		t.Fatalf("expected ipfs_provide to link back to the /api/execute span, got %v", links)
	}
	if !hasAttribute(root.Attributes(), attribute.String("ipfs.provide.trace_id", provide.SpanContext().TraceID().String())) {
		t.Fatal("expected /api/execute to carry the ipfs_provide trace ID")
	}
	if !hasAttribute(provide.Attributes(), attribute.String("code.cid", "bafyFAKECID")) {
		t.Fatalf("expected code.cid on ipfs_provide, got %v", provide.Attributes())
	}
	if provide.Status().Code == codes.Error {
		t.Fatalf("expected a successful ipfs_provide span, got %v", provide.Status())
	}
}

func TestHandler_ProvideFails_ProvideSpanHasErrorStatus(t *testing.T) {
	recorder := recordSpans(t)

	resp := executeWith(t, &fakeUploader{cid: "bafyFAKECID", provideErr: errors.New("no DHT peers")})

	if !resp.Success || resp.IPFSLink == "" {
		t.Fatalf("a failed announcement must not affect the run, got %+v", resp)
	}
	if provide := waitForSpan(t, recorder, "ipfs_provide"); provide.Status().Code != codes.Error {
		t.Fatalf("expected ipfs_provide to have error status, got %v", provide.Status())
	}
}

func hasAttribute(attrs []attribute.KeyValue, want attribute.KeyValue) bool {
	for _, kv := range attrs {
		if kv == want {
			return true
		}
	}
	return false
}
