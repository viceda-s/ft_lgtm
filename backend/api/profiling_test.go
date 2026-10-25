package api

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestHandler_ValidCode_ExecuteSpanCarriesProfilingAttributes(t *testing.T) {
	recorder := recordSpans(t)

	executeWith(t, &fakeUploader{cid: "bafyFAKECID"})

	got := map[attribute.Key]attribute.Value{}
	for _, kv := range waitForSpan(t, recorder, "execute").Attributes() {
		got[kv.Key] = kv.Value
	}
	for _, key := range []attribute.Key{"execution.cpu_time_ms", "wasm.memory_bytes", "wasm.fuel_consumed"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("expected %s on the execute span, got %v", key, got)
		}
	}
	if got["wasm.fuel_consumed"].AsInt64() <= 0 || got["wasm.memory_bytes"].AsInt64() <= 0 {
		t.Fatalf("expected positive fuel and memory, got %v", got)
	}
}
