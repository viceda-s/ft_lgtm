package executor

import (
    "context"
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
