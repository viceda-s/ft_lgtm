package executor

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"ft_lgtm/backend/compiler"
)

func compileFixture(t *testing.T, source string) []byte {
	t.Helper()
	wasmBytes, err := compiler.Compile(context.Background(), "go", source)
	if err != nil {
		t.Fatalf("failed to compile test fixture: %v", err)
	}
	return wasmBytes
}

func TestExecutor_Run_ValidProgram_CapturesStdout(t *testing.T) {
	wasmBytes := compileFixture(t, `package main

func main() {
    println("Hello from wasm")
}
`)
	exec := NewExecutor()
	result, err := exec.Run(wasmBytes, 5*time.Second, 10*1024)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	if result.TimedOut {
		t.Fatalf("expected TimedOut= false for a fast program")
	}
	if result.Stdout == "" {
		t.Fatalf("expected non-empty stdout")
	}
}

func TestExecutor_Run_InfiniteLoop_TimesOut(t *testing.T) {
	wasmBytes := compileFixture(t, `package main

func main() {
    for {
    }
}
`)
	exec := NewExecutor()
	start := time.Now()
	result, err := exec.Run(wasmBytes, 1*time.Second, 10*1024)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("expected TimedOut=true for an infinite loop")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("expected timeout to trigger near the 1s deadline, took %v", elapsed)
	}
}

func TestExecutor_Run_ExcessiveOutput_TruncatesToLimit(t *testing.T) {
	wasmBytes := compileFixture(t, `package main

func main() {
    for i := 0; i < 10000; i++ {
        println("this is a line of output that will be repeated many times to exceed the cap")
    }
}
`)
	exec := NewExecutor()
	result, err := exec.Run(wasmBytes, 5*time.Second, 1024)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	if len(result.Stdout) > 1024 {
		t.Fatalf("expected stdout truncated to at most %d bytes, got %d", 1024, len(result.Stdout))
	}
	if len(result.Stdout) == 0 {
		t.Fatal("expected some stdout captured before truncation, got none")
	}
	if !result.OutputLimitExceeded {
		t.Fatal("expected OutputLimitExceeded=true when the program writes past the cap")
	}
}

func TestExecutor_Run_EndlessOutput_StopsAtLimitInsteadOfRunningToTimeout(t *testing.T) {
	wasmBytes := compileFixture(t, `package main

func main() {
    for {
        println("flooding the output")
    }
}
`)
	exec := NewExecutor()
	start := time.Now()
	result, err := exec.Run(wasmBytes, 5*time.Second, 1024)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	if result.TimedOut {
		t.Fatal("expected the output cap to stop the program before its 5s timeout")
	}
	if !result.OutputLimitExceeded {
		t.Fatal("expected OutputLimitExceeded=true")
	}
	if result.ExitError == nil {
		t.Fatal("expected ExitError to be set when the program is stopped for exceeding the output cap")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected the program to be stopped as soon as it exceeded the cap, took %v", elapsed)
	}
}

func TestExecutor_Run_OneTimeoutDoesNotInterruptConcurrentRuns(t *testing.T) {
	loop := compileFixture(t, `package main

func main() {
    for {
    }
}
`)
	sleeper := compileFixture(t, `package main

import "time"

func main() {
    time.Sleep(time.Second)
    println("done")
}
`)
	exec := NewExecutor()

	var wg sync.WaitGroup
	var sleeperResult Result
	var sleeperErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		sleeperResult, sleeperErr = exec.Run(sleeper, 5*time.Second, 10*1024)
	}()

	loopResult, err := exec.Run(loop, 300*time.Millisecond, 10*1024)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	if !loopResult.TimedOut {
		t.Fatal("expected the infinite loop to time out")
	}

	wg.Wait()
	if sleeperErr != nil {
		t.Fatalf("Run returned infrastructure error: %v", sleeperErr)
	}
	if sleeperResult.TimedOut {
		t.Fatal("expected the concurrent run to finish: another run's timeout must not interrupt it")
	}
	if sleeperResult.ExitError != nil {
		t.Fatalf("expected the concurrent run to exit cleanly, got: %v", sleeperResult.ExitError)
	}
}

func TestExecutor_Run_ExcessiveMemory_FailsRatherThanExhaustingHost(t *testing.T) {
	wasmBytes := compileFixture(t, `package main

func main() {
    var chunks[][]byte
    for i := 0; i < 1000; i++ {
        chunks = append(chunks, make([]byte, 1024*1024)) // 1MB per iteration
    }
    println("should not reach here:", len(chunks))
}
`)

	exec := NewExecutor()
	result, err := exec.Run(wasmBytes, 5*time.Second, 10*1024)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	if result.TimedOut {
		t.Fatal("expected memory limit to trigger before the timeout, not a timeout")
	}
	if result.ExitError == nil {
		t.Fatal("expected ExitError to be set when the module exceeds its memory limit")
	}
}

func TestExecutor_Run_ReportsCPUMemoryAndFuel(t *testing.T) {
	exec := NewExecutor()
	defer exec.Close()
	idle, err := exec.Run(compileFixture(t, "package main\n\nfunc main() {}\n"), 5*time.Second, 10*1024)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	busy, err := exec.Run(compileFixture(t, `package main

func main() {
    total := uint32(1)
    for i := uint32(0); i < 20000000; i++ {
        total = total*1664525 + i
    }
    println(total)
}
`), 5*time.Second, 10*1024)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}

	if busy.FuelConsumed < 10*idle.FuelConsumed || busy.FuelConsumed < 20000000 {
		t.Fatalf("expected the busy loop to consume far more fuel than an empty program, got %d vs %d", busy.FuelConsumed, idle.FuelConsumed)
	}
	if busy.MemoryBytes == 0 {
		t.Fatal("expected the linear memory size to be reported")
	}
	if runtime.GOOS == "linux" && busy.CPUTime <= idle.CPUTime {
		t.Fatalf("expected the busy loop to use more thread CPU time, got %v vs %v", busy.CPUTime, idle.CPUTime)
	}
}
