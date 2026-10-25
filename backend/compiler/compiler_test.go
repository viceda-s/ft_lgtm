package compiler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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
