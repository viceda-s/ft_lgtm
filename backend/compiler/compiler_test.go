package compiler

import (
	"context"
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