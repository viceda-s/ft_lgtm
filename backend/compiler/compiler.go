package compiler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type CompileError struct {
	Stderr string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("tinygo build failed: %s", e.Stderr)
}

func Compile(clx context.Context, source string) ([]byte, error) {
	if f, err := parser.ParseFile(token.NewFileSet(), "main.go", source, parser.ImportsOnly); err == nil {
		for _, imp := range f.Imports {
			if imp.Path.Value == `"C"` {
				return nil, &CompileError{Stderr: "main.go: cgo (import \"C\") is not allowed in the playground"}
			}
		}
	}

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

	cmd := exec.CommandContext(clx, "/usr/local/bin/tinygo", "build", "-o", "main.wasm", "main.go")
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	recordUsage(clx, cmd.ProcessState)
	if err != nil {
		if errors.Is(clx.Err(), context.DeadlineExceeded) {
			return nil, &CompileError{Stderr: "compilation timed out"}
		}
		if stderr.Len() == 0 {
			return nil, fmt.Errorf("running tinygo: %w", err)
		}
		return nil, &CompileError{Stderr: stderr.String()}
	}

	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("reading compiled wasm output: %w", err)
	}

	return wasmBytes, nil
}

func recordUsage(ctx context.Context, state *os.ProcessState) {
	if state == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.Int64("compile.cpu_time_ms", (state.UserTime() + state.SystemTime()).Milliseconds()),
	}
	if usage, ok := state.SysUsage().(*syscall.Rusage); ok {
		attrs = append(attrs, attribute.Int64("compile.max_rss_bytes", usage.Maxrss*1024))
	}
	trace.SpanFromContext(ctx).SetAttributes(attrs...)
}
