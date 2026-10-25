package compiler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestCompile_ValidProgram_ReturnsWasmBytes(t *testing.T) {
	source := `package main

func main() {
	println("Hello")
}
`
	wasmBytes, err := Compile(context.Background(), source)
	if err != nil {
		t.Fatalf("Compile returned error for valid program: %v", err)
	}
	if len(wasmBytes) == 0 {
		t.Fatalf("Compile returned empty wasm bytes for valid program")
	}
	if len(wasmBytes) < 4 || wasmBytes[0] != 0x00 || wasmBytes[1] != 0x61 || wasmBytes[2] != 0x73 || wasmBytes[3] != 0x6D {
		t.Fatalf("Compile output does not start with WASM magic number, got: %v", wasmBytes[:4])
	}
}

func TestCompile_InvalidProgram_ReturnsCompileError(t *testing.T) {
	source := `package main

func main() {
	This is not valid Go syntax
}`
	_, err:= Compile(context.Background(), source)
	if err == nil {
		t.Fatal("expected Compile to return an error for invalid syntax, got nil")
	}

	var compileErr *CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("expected error to be *CompileError, got %T: %v", err, err)
	}
	if compileErr.Stderr == "" {
		t.Fatal("expected CompileError.Stderr to contain TinyGo's error output, got empty string")
	}
}

func TestCompile_InvalidProgram_ErrorReferencesMainGoNotTempDir(t *testing.T) {
	source := `package main

func main() {
	This is not valid Go syntax
}`
	_, err := Compile(context.Background(), source)

	var compileErr *CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("expected error to be *CompileError, got %T: %v", err, err)
	}
	if strings.Contains(compileErr.Stderr, "lgtm-compile-") {
		t.Fatalf("expected the temp build dir to be stripped from the error, got: %s", compileErr.Stderr)
	}
	if !strings.Contains(compileErr.Stderr, "main.go:") {
		t.Fatalf("expected the error to point at main.go:<line>, got: %s", compileErr.Stderr)
	}
}

func TestCompile_DeadlineExceeded_ReturnsTimeoutCompileError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, err := Compile(ctx, "package main\n\nfunc main() {}\n")

	var compileErr *CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("expected error to be *CompileError, got %T: %v", err, err)
	}
	if !strings.Contains(compileErr.Stderr, "timed out") {
		t.Fatalf("expected a timeout message, got: %s", compileErr.Stderr)
	}
}

func TestCompile_Cgo_RejectedBeforeIncludingHostFiles(t *testing.T) {
	source := "package main\n\n// #include \"/etc/hostname\"\nimport \"C\"\n\nfunc main() {}\n"
	_, err := Compile(context.Background(), source)

	var compileErr *CompileError
	if !errors.As(err, &compileErr) {
		t.Fatalf("expected error to be *CompileError, got %T: %v", err, err)
	}
	if !strings.Contains(compileErr.Stderr, "cgo") {
		t.Fatalf("expected a cgo rejection message, got: %s", compileErr.Stderr)
	}
}

func TestCompile_RecordsCompilerCPUAndMemoryOnSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	ctx, span := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test").Start(context.Background(), "compile")

	if _, err := Compile(ctx, "package main\n\nfunc main() {}\n"); err != nil {
		t.Fatalf("Compile returned error for valid program: %v", err)
	}
	span.End()

	got := map[attribute.Key]attribute.Value{}
	for _, kv := range recorder.Ended()[0].Attributes() {
		got[kv.Key] = kv.Value
	}
	if _, ok := got["compile.cpu_time_ms"]; !ok {
		t.Fatalf("expected compile.cpu_time_ms on the span, got %v", got)
	}
	if rss, ok := got["compile.max_rss_bytes"]; !ok || rss.AsInt64() < 1<<20 {
		t.Fatalf("expected compile.max_rss_bytes of at least 1 MiB, got %v", got)
	}
}
