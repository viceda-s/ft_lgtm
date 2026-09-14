package compiler

import (
	"context"
	"errors"
	"testing"
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
	// WASM binary magic number: 0x00 0x61 0x73 0x6D
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