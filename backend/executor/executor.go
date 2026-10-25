package executor

import (
    "bytes"
    "encoding/binary"
    "fmt"
    "runtime"
    "time"
    "unicode/utf16"

    "github.com/bytecodealliance/wasmtime-go/v48"
)


const epochTick = 10 * time.Millisecond

const fuelBudget = 1 << 62

const (
    wasiErrnoSuccess = 0
    wasiErrnoBadf    = 8
    wasiErrnoFault   = 21
)


type Result struct {
    Stdout              string
    Stderr              string
    TimedOut            bool
    OutputLimitExceeded bool
    ExitError           error
    CPUTime             time.Duration
    MemoryBytes         uint64
    FuelConsumed        uint64
}


type Executor struct {
    engine *wasmtime.Engine
    stop   chan struct{}
}


func NewExecutor() *Executor {
    config := wasmtime.NewConfig()
    config.SetEpochInterruption(true)
    config.SetConsumeFuel(true)
    e := &Executor{
        engine: wasmtime.NewEngineWithConfig(config),
        stop:   make(chan struct{}),
    }
    go e.tickEpochs()
    return e
}


func (e *Executor) tickEpochs() {
    ticker := time.NewTicker(epochTick)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            e.engine.IncrementEpoch()
        case <-e.stop:
            return
        }
    }
}


func (e *Executor) Close() {
    close(e.stop)
}


func (e *Executor) Run(wasmBytes []byte, timeout time.Duration, maxOutputBytes int) (Result, error) {
    store := wasmtime.NewStore(e.engine)
    defer store.Close()
    store.SetEpochDeadline(uint64(timeout/epochTick) + 1)
    store.Limiter(int64(10*1024*1024), -1, -1, -1, -1)
    if err := store.SetFuel(fuelBudget); err != nil {
        return Result{}, fmt.Errorf("setting fuel: %w", err)
    }

    wasiConfig := wasmtime.NewWasiConfig()
    wasiConfig.SetArgv([]string{"prog"})
    store.SetWasi(wasiConfig)

    output := &outputCapture{limit: maxOutputBytes, remaining: maxOutputBytes}

    linker := wasmtime.NewLinker(e.engine)
    defer linker.Close()
    if err := linker.DefineWasi(); err != nil {
        return Result{}, fmt.Errorf("defining WASI imports: %w", err)
    }
    linker.AllowShadowing(true)
    if err := linker.FuncWrap("wasi_snapshot_preview1", "fd_write", output.fdWrite); err != nil {
        return Result{}, fmt.Errorf("overriding WASI fd_write: %w", err)
    }
    if err := linker.FuncWrap("env", "console.log", output.consoleLog); err != nil {
        return Result{}, fmt.Errorf("defining env.console.log: %w", err)
    }
    if err := linker.FuncWrap("env", "abort", output.abort); err != nil {
        return Result{}, fmt.Errorf("defining env.abort: %w", err)
    }
    if err := linker.FuncWrap("env", "seed", func() float64 { return float64(time.Now().UnixNano()) }); err != nil {
        return Result{}, fmt.Errorf("defining env.seed: %w", err)
    }

    module, err := wasmtime.NewModule(e.engine, wasmBytes)
    if err != nil {
        return Result{}, fmt.Errorf("loading wasm module: %w", err)
    }

    instance, err := linker.Instantiate(store, module)
    if err != nil {
        return Result{ExitError: fmt.Errorf("instantiating wasm module: %w", err)}, nil
    }

    start := instance.GetFunc(store, "_start")
    if start == nil {
        return Result{}, fmt.Errorf("wasm module has no _start function (not a WASI command module)")
    }

    runtime.LockOSThread()
    cpuBefore := threadCPUTime()
    _, callErr := start.Call(store)
    cpuTime := threadCPUTime() - cpuBefore
    runtime.UnlockOSThread()

    result := Result{
        Stdout:              output.stdout.String(),
        Stderr:              output.stderr.String(),
        OutputLimitExceeded: output.exceeded,
        CPUTime:             cpuTime,
    }
    if remaining, err := store.GetFuel(); err == nil {
        result.FuelConsumed = fuelBudget - remaining
    }
    if export := instance.GetExport(store, "memory"); export != nil && export.Memory() != nil {
        result.MemoryBytes = uint64(export.Memory().DataSize(store))
    }
    result.TimedOut, result.ExitError = classifyCallError(callErr, output.exceeded)
    return result, nil
}


func classifyCallError(callErr error, outputExceeded bool) (timedOut bool, exitErr error) {
    if callErr == nil {
        return false, nil
    }
    if outputExceeded {
        return false, callErr
    }
    if trap, ok := callErr.(*wasmtime.Trap); ok && trap.Code() != nil && *trap.Code() == wasmtime.Interrupt {
        return true, nil
    }
    if wasmErr, ok := callErr.(*wasmtime.Error); ok {
        if exitCode, isExit := wasmErr.ExitStatus(); isExit && exitCode == 0 {
            return false, nil
        }
    }
    return false, callErr
}


type outputCapture struct {
    stdout    bytes.Buffer
    stderr    bytes.Buffer
    limit     int
    remaining int
    exceeded  bool
}


func (o *outputCapture) fdWrite(caller *wasmtime.Caller, fd, iovs, iovsLen, nwritten int32) (int32, *wasmtime.Trap) {
    var dst *bytes.Buffer
    switch fd {
    case 1:
        dst = &o.stdout
    case 2:
        dst = &o.stderr
    default:
        return wasiErrnoBadf, nil
    }

    memExport := caller.GetExport("memory")
    if memExport == nil || memExport.Memory() == nil {
        return wasiErrnoFault, nil
    }
    mem := memExport.Memory().UnsafeData(caller)

    var written uint32
    for i := uint32(0); i < uint32(iovsLen); i++ {
        iov, ok := memSlice(mem, uint32(iovs)+i*8, 8)
        if !ok {
            return wasiErrnoFault, nil
        }
        data, ok := memSlice(mem, binary.LittleEndian.Uint32(iov[0:4]), binary.LittleEndian.Uint32(iov[4:8]))
        if !ok {
            return wasiErrnoFault, nil
        }
        if trap := o.write(dst, data); trap != nil {
            return 0, trap
        }
        written += uint32(len(data))
    }

    out, ok := memSlice(mem, uint32(nwritten), 4)
    if !ok {
        return wasiErrnoFault, nil
    }
    binary.LittleEndian.PutUint32(out, written)
    return wasiErrnoSuccess, nil
}


func (o *outputCapture) write(dst *bytes.Buffer, data []byte) *wasmtime.Trap {
    if len(data) > o.remaining {
        dst.Write(data[:o.remaining])
        o.remaining = 0
        o.exceeded = true
        return wasmtime.NewTrap(fmt.Sprintf("output limit of %d bytes exceeded", o.limit))
    }
    dst.Write(data)
    o.remaining -= len(data)
    return nil
}


func (o *outputCapture) consoleLog(caller *wasmtime.Caller, message int32) *wasmtime.Trap {
    text, ok := readASString(caller, message)
    if !ok {
        return wasmtime.NewTrap("console.log: string is out of bounds")
    }
    return o.write(&o.stdout, []byte(text+"\n"))
}


func (o *outputCapture) abort(caller *wasmtime.Caller, message, file, line, column int32) *wasmtime.Trap {
    text, _ := readASString(caller, message)
    name, _ := readASString(caller, file)
    if trap := o.write(&o.stderr, []byte(fmt.Sprintf("abort: %s at %s:%d:%d\n", text, name, line, column))); trap != nil {
        return trap
    }
    return wasmtime.NewTrap("abort: " + text)
}


func readASString(caller *wasmtime.Caller, ptr int32) (string, bool) {
    if ptr == 0 {
        return "", true
    }
    memExport := caller.GetExport("memory")
    if ptr < 4 || memExport == nil || memExport.Memory() == nil {
        return "", false
    }
    mem := memExport.Memory().UnsafeData(caller)
    header, ok := memSlice(mem, uint32(ptr)-4, 4)
    if !ok {
        return "", false
    }
    data, ok := memSlice(mem, uint32(ptr), binary.LittleEndian.Uint32(header))
    if !ok || len(data)%2 != 0 {
        return "", false
    }
    units := make([]uint16, len(data)/2)
    for i := range units {
        units[i] = binary.LittleEndian.Uint16(data[2*i:])
    }
    return string(utf16.Decode(units)), true
}


func memSlice(mem []byte, offset, length uint32) ([]byte, bool) {
    end := uint64(offset) + uint64(length)
    if end > uint64(len(mem)) {
        return nil, false
    }
    return mem[offset:end], true
}
