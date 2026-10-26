package executor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"ft_lgtm/backend/compiler"
)

func TestMain(m *testing.M) {
	compiler.RunSandboxIfRequested()
	os.Exit(m.Run())
}

func runProgram(t *testing.T, lang, source string) Result {
	t.Helper()
	file, _ := compiler.SourceFile(lang)
	wasm, err := compiler.Compile(context.Background(), lang, source)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "landlock") {
			t.Skipf("%s toolchain unavailable: %v", file, err)
		}
		t.Fatalf("compiling %s: %v", file, err)
	}
	exec := NewExecutor()
	defer exec.Close()
	result, err := exec.Run(wasm, 5*time.Second, 10*1024)
	if err != nil {
		t.Fatalf("Run returned infrastructure error: %v", err)
	}
	return result
}

func TestExecutor_Run_EachLanguage_PrintsToStdout(t *testing.T) {
	programs := map[string]string{
		"c":              "#include <stdio.h>\nint main(void) { printf(\"hello %d\\n\", 6 * 7); return 0; }\n",
		"rust":           "fn main() { println!(\"hello {}\", 6 * 7); }\n",
		"assemblyscript": "console.log(\"hello \" + (6 * 7).toString());\n",
	}
	for lang, source := range programs {
		t.Run(lang, func(t *testing.T) {
			result := runProgram(t, lang, source)
			if result.ExitError != nil || result.Stdout != "hello 42\n" {
				t.Fatalf("expected stdout %q, got %q (exit error: %v, stderr: %q)", "hello 42\n", result.Stdout, result.ExitError, result.Stderr)
			}
		})
	}
}

func TestExecutor_Run_EachLanguage_ReportsRuntimeFailures(t *testing.T) {
	programs := map[string]string{
		"c":              "#include <stdlib.h>\n#include <stdio.h>\nint main(void) { fprintf(stderr, \"boom\\n\"); exit(3); }\n",
		"rust":           "fn main() { panic!(\"boom\"); }\n",
		"assemblyscript": "assert(false, \"boom\");\n",
	}
	for lang, source := range programs {
		t.Run(lang, func(t *testing.T) {
			result := runProgram(t, lang, source)
			if result.ExitError == nil || !strings.Contains(result.Stderr, "boom") {
				t.Fatalf("expected a runtime failure mentioning boom, got exit error %v and stderr %q", result.ExitError, result.Stderr)
			}
		})
	}
}

func TestExecutor_Run_AssemblyScriptConsole_RespectsOutputLimit(t *testing.T) {
	result := runProgram(t, "assemblyscript", "while (true) { console.log(\"spam spam spam spam\"); }\n")
	if !result.OutputLimitExceeded || len(result.Stdout) > 10*1024 {
		t.Fatalf("expected the output limit to stop the program, got %d bytes (exceeded=%v)", len(result.Stdout), result.OutputLimitExceeded)
	}
}

func TestExecutor_Run_CAbsoluteSleep_RespectsDeadline(t *testing.T) {
	sleep := func(clock string, seconds, nanoseconds int) string {
		return fmt.Sprintf("#include <stdio.h>\n#include <time.h>\nint main(void) {\n"+
			"  struct timespec t;\n  clock_gettime(%s, &t);\n  t.tv_sec += %d; t.tv_nsec += %d;\n"+
			"  if (t.tv_nsec >= 1000000000) { t.tv_sec++; t.tv_nsec -= 1000000000; }\n"+
			"  clock_nanosleep(%s, TIMER_ABSTIME, &t, NULL);\n  puts(\"woke up\");\n  return 0;\n}\n", clock, seconds, nanoseconds, clock)
	}
	for _, clock := range []string{"CLOCK_MONOTONIC", "CLOCK_REALTIME"} {
		t.Run(clock, func(t *testing.T) {
			if result := runProgram(t, "c", sleep(clock, 0, 50_000_000)); result.TimedOut || result.Stdout != "woke up\n" {
				t.Fatalf("expected a 50ms absolute sleep to finish, got %+v", result)
			}
			start := time.Now()
			if result := runProgram(t, "c", sleep(clock, 30, 0)); !result.TimedOut || time.Since(start) > 5*time.Second {
				t.Fatalf("expected a 30s absolute sleep to time out at once, got %+v after %v", result, time.Since(start))
			}
		})
	}
}

func TestExecutor_Run_CTableGrowth_IsCapped(t *testing.T) {
	result := runProgram(t, "c", "#include <stdio.h>\ntypedef void (*__funcref funcref_t)(void);\nstatic funcref_t table[0];\n"+
		"int main(void) {\n  printf(\"%d\\n\", __builtin_wasm_table_grow(table, __builtin_wasm_ref_null_func(), 50000000));\n  return 0;\n}\n")
	if result.Stdout == "0\n" {
		t.Fatalf("expected growing a table to 50M elements to fail, got %+v", result)
	}
}

func TestExecutor_Run_CPollWithOverflowingCount_ReturnsFault(t *testing.T) {
	result := runProgram(t, "c", "#include <stdio.h>\n#include <wasi/api.h>\nint main(void) {\n"+
		"  __wasi_subscription_t sub = {0};\n  __wasi_event_t event;\n  __wasi_size_t n;\n"+
		"  printf(\"%d\\n\", __wasi_poll_oneoff(&sub, &event, 0x10000000, &n));\n  return 0;\n}\n")
	if result.ExitError != nil || result.Stdout != "21\n" {
		t.Fatalf("expected EFAULT (21) for a subscription count that overflows, got %+v", result)
	}
}
