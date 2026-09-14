package executor

import (
    "fmt"
    "os"
    "time"

    "github.com/bytecodealliance/wasmtime-go/v48"
)


// Result holds the outcome of running one WASM module.
type Result struct {
    Stdout    string
    Stderr    string
    TimedOut  bool
    ExitError error // the wasm module's own trap/exit error, if any (nil on clean exit)
}


// Executor owns a single long-lived Wasmtime Engine shared across all runs.
// Engine creation is expensive; Engine is documented as safe to share across goroutines, so one Executor should be constructed once per process.
type Executor struct {
    engine *wasmtime.Engine
}


// NewExecutor builds the shared engine with epoch interruption enabled.
// Epoch interruption is how per-run timeouts are enforced: each Run creates its own Store with a near-term epoch deadline, and a goroutine ticks the shared engine's epoch forward after the caller's timeout elapses.
func NewExecutor() *Executor {
    config := wasmtime.NewConfig()
    config.SetEpochInterruption(true)
    engine := wasmtime.NewEngineWithConfig(config)
    return &Executor{engine: engine}
}


// Run instantiates wasmBytes in a fresh, sandboxed Store and calls its  WASI command entrypoint (_start). The sandbox grants no filesystem  preopens and no network capability (both achieved by omission — this  WasiConfig never calls PreopenDir, and plain WASI preview 1 command  modules have no socket imports to begin with), caps linear memory at  ~10MB, and enforces timeout via epoch interruption. Stdout/stderr are  captured through temp files (wasmtime-go's WasiConfig only supports  file-path-based output capture, not an in-process io.Writer) and  truncated to maxOutputBytes after the run completes.
func (e *Executor) Run(wasmBytes []byte, timeout time.Duration, maxOutputBytes int) (Result, error) {
    store := wasmtime.NewStore(e.engine)
    store.SetEpochDeadline(1)
    store.Limiter(int64(10*1024*1024), -1, -1, -1, -1) // ~10MB memory cap, other resources unlimited

    stdoutFile, err := os.CreateTemp("", "lgtm-stdout-*")
    if err != nil {
        return Result{}, fmt.Errorf("creating stdout temp file: %w", err)
    }
    defer os.Remove(stdoutFile.Name())
    defer stdoutFile.Close()

    stderrFile, err := os.CreateTemp("", "lgtm-stderr-*")
    if err != nil {
        return Result{}, fmt.Errorf("creating stderr temp file: %w", err)
    }
    defer os.Remove(stderrFile.Name())
    defer stderrFile.Close()

    wasiConfig := wasmtime.NewWasiConfig()
    wasiConfig.SetArgv([]string{"prog"})
    if err := wasiConfig.SetStdoutFile(stdoutFile.Name()); err != nil {
        return Result{}, fmt.Errorf("wiring stdout capture: %w", err)
    }
    if err := wasiConfig.SetStderrFile(stderrFile.Name()); err != nil {
        return Result{}, fmt.Errorf("wiring stderr capture: %w", err)
    }
    store.SetWasi(wasiConfig)

    linker := wasmtime.NewLinker(e.engine)
    if err := linker.DefineWasi(); err != nil {
        return Result{}, fmt.Errorf("defining WASI imports: %w", err)
    }

    module, err := wasmtime.NewModule(e.engine, wasmBytes)
    if err != nil {
        return Result{}, fmt.Errorf("loading wasm module: %w", err)
    }

    instance, err := linker.Instantiate(store, module)
    if err != nil {
        return Result{}, fmt.Errorf("instantiating wasm module: %w", err)
    }

    start := instance.GetFunc(store, "_start")
    if start == nil {
        return Result{}, fmt.Errorf("wasm module has no _start function (not a WASI command module)")
    }

    deadline := make(chan struct{})
    go func() {
        select {
        case <-time.After(timeout):
            e.engine.IncrementEpoch()
        case <-deadline:
        }
    }()

    _, callErr := start.Call(store)
    close(deadline)

    result := Result{
        Stdout: readTruncated(stdoutFile.Name(), maxOutputBytes),
        Stderr: readTruncated(stderrFile.Name(), maxOutputBytes),
    }

    if callErr != nil {
        if trap, ok := callErr.(*wasmtime.Trap); ok && trap.Code() != nil && *trap.Code() == wasmtime.Interrupt {
            result.TimedOut = true
        } else if wasmErr, ok := callErr.(*wasmtime.Error); ok {
            if exitCode, isExit := wasmErr.ExitStatus(); isExit {
                if exitCode != 0 {
                    result.ExitError = callErr
                }
                // exitCode == 0: clean WASI exit, not an error
            } else {
                result.ExitError = callErr
            }
        } else {
            result.ExitError = callErr
        }
    }
    return result, nil
}


// readTruncated reads up to maxBytes from the file at path. Any read error (including the file being empty or missing content) yields an empty string rather than propagating, since a truncated/absent output stream is not an infrastructure failure.
func readTruncated(path string, maxBytes int) string {
    f, err := os.Open(path)
    if err != nil {
        return ""
    }
    defer f.Close()

    buf := make([]byte, maxBytes)
    n, _ := f.Read(buf)
    return string(buf[:n])
}
