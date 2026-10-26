package executor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"slices"
	"time"
	"unicode/utf16"

	"github.com/bytecodealliance/wasmtime-go/v48"
)

const epochTick = 10 * time.Millisecond

const fuelBudget = 1 << 62

const maxTableElements = 10000

const (
	wasiErrnoSuccess = 0
	wasiErrnoBadf    = 8
	wasiErrnoFault   = 21
	wasiErrnoInval   = 28
)

const (
	wasiSubscriptionSize = 48
	wasiEventSize        = 32
	wasiEventtypeClock   = 0
	wasiClockRealtime    = 0
	wasiSubclockAbstime  = 1
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
	sleeps := &sleepLimit{deadline: time.Now().Add(timeout)}
	store.SetEpochDeadline(uint64(timeout/epochTick) + 1)
	// tables live in host memory too, so they are capped alongside the 10 MiB of linear memory
	store.Limiter(int64(10*1024*1024), maxTableElements, -1, 1, 1)
	if err := store.SetFuel(fuelBudget); err != nil {
		return Result{}, fmt.Errorf("setting fuel: %w", err)
	}

	wasiConfig := wasmtime.NewWasiConfig()
	wasiConfig.SetArgv([]string{"prog"})
	sleeps.wasiStart = time.Now()
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
	if err := linker.FuncWrap("wasi_snapshot_preview1", "poll_oneoff", sleeps.pollOneoff); err != nil {
		return Result{}, fmt.Errorf("overriding WASI poll_oneoff: %w", err)
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
	if sleeps.exceeded {
		result.TimedOut, result.ExitError = true, nil
	}
	return result, nil
}

// sleepLimit replaces WASI's poll_oneoff: a sleep blocks on the host, where epoch interruption cannot stop it,
// so a sleep that would end past the deadline stops the program at once instead of holding a run slot
type sleepLimit struct {
	deadline  time.Time
	wasiStart time.Time
	exceeded  bool
}

func (s *sleepLimit) pollOneoff(caller *wasmtime.Caller, in, out, nsubscriptions, nevents int32) (int32, *wasmtime.Trap) {
	if nsubscriptions <= 0 {
		return wasiErrnoInval, nil
	}
	memExport := caller.GetExport("memory")
	if memExport == nil || memExport.Memory() == nil {
		return wasiErrnoFault, nil
	}
	mem := memExport.Memory().UnsafeData(caller)
	if int(nsubscriptions) > len(mem)/wasiSubscriptionSize {
		return wasiErrnoFault, nil
	}
	subscriptions, ok := memSlice(mem, uint32(in), uint32(nsubscriptions)*wasiSubscriptionSize)
	if !ok {
		return wasiErrnoFault, nil
	}
	events, ok := memSlice(mem, uint32(out), uint32(nsubscriptions)*wasiEventSize)
	if !ok {
		return wasiErrnoFault, nil
	}
	count, ok := memSlice(mem, uint32(nevents), 4)
	if !ok {
		return wasiErrnoFault, nil
	}

	// file descriptors are ready at once: stdin is empty and stdout/stderr always accept writes
	now := time.Now()
	readyAt := make([]time.Time, nsubscriptions)
	for i := range readyAt {
		readyAt[i] = now
		if sub := subscriptions[i*wasiSubscriptionSize:]; sub[8] == wasiEventtypeClock {
			readyAt[i] = s.clockDeadline(sub, now)
		}
	}
	wake := slices.MinFunc(readyAt, time.Time.Compare)
	if wake.After(s.deadline) {
		s.exceeded = true
		return 0, wasmtime.NewTrap("time limit exceeded while sleeping")
	}
	time.Sleep(time.Until(wake))

	n := 0
	for i, at := range readyAt {
		if at.After(wake) {
			continue
		}
		sub, event := subscriptions[i*wasiSubscriptionSize:], events[n*wasiEventSize:(n+1)*wasiEventSize]
		clear(event)
		copy(event[0:8], sub[0:8])
		event[10] = sub[8]
		if sub[8] != wasiEventtypeClock && binary.LittleEndian.Uint32(sub[16:20]) > 2 {
			binary.LittleEndian.PutUint16(event[8:10], wasiErrnoBadf)
		}
		n++
	}
	binary.LittleEndian.PutUint32(count, uint32(n))
	return wasiErrnoSuccess, nil
}

func (s *sleepLimit) clockDeadline(sub []byte, now time.Time) time.Time {
	timeout := time.Duration(min(binary.LittleEndian.Uint64(sub[24:32]), math.MaxInt64))
	if binary.LittleEndian.Uint16(sub[40:42])&wasiSubclockAbstime == 0 {
		return now.Add(timeout)
	}
	if binary.LittleEndian.Uint32(sub[16:20]) == wasiClockRealtime {
		return time.Unix(0, int64(timeout))
	}
	// wasmtime's monotonic clock counts from when the WASI context was built
	return s.wasiStart.Add(timeout)
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
