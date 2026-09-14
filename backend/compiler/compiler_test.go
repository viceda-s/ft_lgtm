package compiler

import (
	"context"
	"testing"
)

func TestCompile_ValideProgram_ReturnsWasmBytes(t *testing.T) {
	source := `package main

func main() {
	println("Hello")
}
`
	wasmByetes, err := Compile(context.Background(), source)
	if err != nil {
		t.Fatalf("Compile returned error for valid program: %v", err)
	}
	if len(wasmByetes) == 0 {
		t.Fatalf("Compile returned empty wasm bytes for valid program")
	}
	// WASM binary magic number: 0x00 0x61 0x73 0x6D
	if len(wasmByetes) < 4 || wasmByetes[0] != 0x00 || wasmByetes[1] != 0x61 || wasmByetes[2] != 0x73 || wasmByetes[3] != 0x6D {
		t.Fatalf("Compile output does not start with WASM magic number, got: %v", wasmByetes[:4])
	}
}