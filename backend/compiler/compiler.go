package compiler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type CompileError struct {
	Stderr string
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("compilation failed: %s", e.Stderr)
}

type language struct {
	source   string
	command  []string
	readOnly []string
	check    func(source string) error
}

var languages = map[string]language{
	"go": {
		source:  "main.go",
		command: []string{"/usr/local/bin/tinygo", "build", "-o", "main.wasm", "main.go"},
		check:   rejectCgo,
	},
	"c": {
		source:   "main.c",
		command:  []string{"/opt/wasi-sdk/bin/clang", "--target=wasm32-wasip1", "-O2", "-o", "main.wasm", "main.c", "-lm"},
		readOnly: []string{"/opt/wasi-sdk"},
	},
	"rust": {
		source:   "main.rs",
		command:  []string{"/opt/rust/bin/rustc", "--edition=2024", "--target=wasm32-wasip1", "-Copt-level=2", "-o", "main.wasm", "main.rs"},
		readOnly: []string{"/opt/rust"},
	},
	"assemblyscript": {
		source:   "main.ts",
		command:  []string{"/usr/local/bin/node", "/opt/assemblyscript/node_modules/assemblyscript/bin/asc.js", "main.ts", "--exportStart", "_start", "-O", "-o", "main.wasm"},
		readOnly: []string{"/usr/local/bin/node", "/opt/assemblyscript"},
	},
}

var systemReadOnly = []string{"/lib", "/lib64", "/usr/lib", "/etc/ld.so.cache", "/dev/urandom"}

const sandboxFailedExitCode = 125

func Languages() []string {
	return slices.Sorted(maps.Keys(languages))
}

func SourceFile(lang string) (string, bool) {
	l, ok := languages[lang]
	return l.source, ok
}

func LanguageOf(file string) (string, bool) {
	for name, l := range languages {
		if l.source == file {
			return name, true
		}
	}
	return "", false
}

func Compile(clx context.Context, lang, source string) ([]byte, error) {
	l, ok := languages[lang]
	if !ok {
		return nil, fmt.Errorf("unsupported language %q", lang)
	}
	if l.check != nil {
		if err := l.check(source); err != nil {
			return nil, err
		}
	}

	tmpDir, err := os.MkdirTemp("", "lgtm-compile-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, l.source), []byte(source), 0o644); err != nil {
		return nil, fmt.Errorf("writing source file: %w", err)
	}

	cmd, err := compileCommand(clx, l, tmpDir)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err = cmd.Run()
	recordUsage(clx, cmd.ProcessState)
	if err != nil {
		if errors.Is(clx.Err(), context.DeadlineExceeded) {
			return nil, &CompileError{Stderr: "compilation timed out"}
		}
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == sandboxFailedExitCode {
			return nil, fmt.Errorf("compiler sandbox: %s", strings.TrimSpace(output.String()))
		}
		if output.Len() == 0 {
			return nil, fmt.Errorf("running compiler: %w", err)
		}
		return nil, &CompileError{Stderr: output.String()}
	}

	wasmBytes, err := os.ReadFile(filepath.Join(tmpDir, "main.wasm"))
	if err != nil {
		return nil, fmt.Errorf("reading compiled wasm output: %w", err)
	}

	return wasmBytes, nil
}

func compileCommand(ctx context.Context, l language, tmpDir string) (*exec.Cmd, error) {
	if l.readOnly == nil {
		cmd := exec.CommandContext(ctx, l.command[0], l.command[1:]...)
		cmd.Dir = tmpDir
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		return cmd, nil
	}

	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locating the sandbox helper: %w", err)
	}
	readOnly := strings.Join(append(append([]string{}, l.readOnly...), systemReadOnly...), ":")
	args := append([]string{sandboxArg, readOnly, tmpDir + ":/dev/null"}, l.command...)
	cmd := exec.CommandContext(ctx, self, args...)
	cmd.Dir = tmpDir
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + tmpDir, "TMPDIR=" + tmpDir, "OPENSSL_CONF=/dev/null"}
	return cmd, nil
}

func rejectCgo(source string) error {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", source, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	for _, imp := range f.Imports {
		if imp.Path.Value == `"C"` {
			return &CompileError{Stderr: "main.go: cgo (import \"C\") is not allowed in the playground"}
		}
	}
	return nil
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
