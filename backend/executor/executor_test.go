package executor

import (
	"context"
	"testing"
	"time"

	"ft_lgtm/backend/compiler"
)

func compileFixture(t *testing.T, source string) []byte {
	t.Helper()
	wasmBytes, err := compiler.Compile(context.Background(), source)
	if err != nil {
		t.Fatalf("failed to compile test fixture: %v", err)
	}
	return wasmBytes
}

func TestExecutor_Run_ValidProgram_CapturesStdout(t *testing.T) {
	wasmBytes := compileFixture(t, `package main

func main() {
	println("Hello from wasm")
}
`)
	exec := NewExecutor()
	result, err := exec.Run(wasmBytes, 5*time.Second, 10*1024)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	if result.TimedOut {
		t.Fatalf("expected TimedOut= false for a fast program")
	}
	if result.Stdout == "" {
		t.Fatalf("expected non-empty stdout")
	}
}