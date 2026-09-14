package executor

import (
    "context"
    "testing"
    "time"

    "ft_lgtm/backend/compiler"
)


func compileFixture(t *testing.T, source string) []byte {
    t.Helper()
    wasmBytes, err := compiler.Compile(context.Background(), source)
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
    result, err := exec.Run(wasmBytes, 5*time.Second, 1024) // 1KB max output
    if err != nil {
        t.Fatalf("Run returned infrastructure error: %v", err)
    }
    if len(result.Stdout) > 1024 {
        t.Fatalf("expected stdout truncated to at most %d bytes, got %d", 1024, len(result.Stdout))
    }
    if len(result.Stdout) == 0 {
        t.Fatal("expected some stdout captured before truncation, got none")
    }
}


func TestExecutor_Run_ExcessiveMemory_FailsRatherThanExhaustingHost(t *testing.T) {
    // Allocates a large slice repeatedly to exceed the ~10MB store limit.
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