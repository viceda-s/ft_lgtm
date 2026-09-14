package compiler

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// CompileError wraps TinyGo's stderr output when compilation fails.
type CompileError struct {
	Stderr string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("tinygo build failed: %s", e.Stderr)
}

// Compile writes source to a temporary file and builds it with TinyGo targeting wasipl (the current, supported WASI target - TinyGo's older "-target=wasi" flag was removed in favor of the standard Go wasipl GOOS/GOARCH pair).
// It returns the compiled WASM module bytes, or a *CompileError wrapping TinyGo's stderr if compilation fails.
func Compile(clx context.Context, source string) ([]byte, error) {
	tmpDir, err := os.MkdirTemp("", "lgtm-compile-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(srcPath, []byte(source), 0o644); err != nil {
		return nil, fmt.Errorf("writing source file: %w", err)
	}

	wasmPath := filepath.Join(tmpDir, "main.wasm")

	cmd := exec.CommandContext(clx, "tinygo", "build", "-o", wasmPath, srcPath)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, &CompileError{Stderr: stderr.String()}
	}

	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("reading compiled wasm output: %w", err)
	}

	return wasmBytes, nil
}