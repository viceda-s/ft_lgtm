package compiler

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	RunSandboxIfRequested()
	os.Exit(m.Run())
}

func requireToolchain(t *testing.T, lang string) {
	t.Helper()
	if _, err := os.Stat(languages[lang].command[0]); err != nil {
		t.Skipf("%s toolchain not installed: %v", lang, err)
	}
}

var helloPrograms = map[string]string{
	"c":              "#include <stdio.h>\nint main(void) { printf(\"hello %d\\n\", 6 * 7); return 0; }\n",
	"rust":           "fn main() { println!(\"hello {}\", 6 * 7); }\n",
	"assemblyscript": "console.log(\"hello \" + (6 * 7).toString());\n",
}

var brokenPrograms = map[string]string{
	"c":              "int main(void) { return undefined_name; }\n",
	"rust":           "fn main() { let x: u32 = \"text\"; }\n",
	"assemblyscript": "let x: i32 = \"text\";\n",
}

func TestCompile_EachLanguage_ProducesWasm(t *testing.T) {
	for lang, source := range helloPrograms {
		t.Run(lang, func(t *testing.T) {
			requireToolchain(t, lang)
			wasm, err := Compile(context.Background(), lang, source)
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
			if len(wasm) < 4 || string(wasm[:4]) != "\x00asm" {
				t.Fatalf("expected a wasm module, got %d bytes", len(wasm))
			}
		})
	}
}

func TestCompile_EachLanguage_ReportsCompileErrors(t *testing.T) {
	for lang, source := range brokenPrograms {
		t.Run(lang, func(t *testing.T) {
			requireToolchain(t, lang)
			_, err := Compile(context.Background(), lang, source)
			var compileErr *CompileError
			if !errors.As(err, &compileErr) || compileErr.Stderr == "" {
				t.Fatalf("expected a CompileError with diagnostics, got %v", err)
			}
			if strings.Contains(compileErr.Stderr, "lgtm-compile-") {
				t.Fatalf("diagnostics leak the temp dir path: %s", compileErr.Stderr)
			}
		})
	}
}

func TestCompile_SandboxBlocksReadingHostFiles(t *testing.T) {
	attacks := []struct {
		name, lang, source string
	}{
		{"c include", "c", "#include \"/etc/passwd\"\nint main(void) { return 0; }\n"},
		{"c computed include", "c", "#define F(x) x\n#include F(\"/etc/passwd\")\nint main(void) { return 0; }\n"},
		{"c embed", "c", "#include <stdio.h>\nstatic const char d[] = {\n#embed \"/etc/passwd\"\n, 0};\nint main(void) { puts(d); return 0; }\n"},
		{"c incbin", "c", "#include <stdio.h>\n__asm__(\".section .rodata.leak,\\\"\\\",@\\n.globl leak\\nleak:\\n.incbin \\\"/etc/passwd\\\"\\n.int8 0\\n.size leak, 64\\n\");\nextern const char leak[];\nint main(void) { puts(leak); return 0; }\n"},
		{"c process environment", "c", "#include \"/proc/self/environ\"\nint main(void) { return 0; }\n"},
		{"rust include_str", "rust", "fn main() { print!(\"{}\", include_str!(\"/etc/passwd\")); }\n"},
		{"rust module path", "rust", "#[path = \"/etc/passwd\"]\nmod leak;\nfn main() {}\n"},
		{"assemblyscript import", "assemblyscript", "import { x } from \"../../../../etc/passwd\";\nconsole.log(x);\n"},
	}
	for _, a := range attacks {
		t.Run(a.name, func(t *testing.T) {
			requireToolchain(t, a.lang)
			wasm, err := Compile(context.Background(), a.lang, a.source)
			if err == nil {
				t.Fatalf("expected the compile to fail, got a %d-byte module", len(wasm))
			}
			if strings.Contains(err.Error(), "root:") {
				t.Fatalf("compile output leaked /etc/passwd: %v", err)
			}
			var compileErr *CompileError
			if !errors.As(err, &compileErr) {
				t.Fatalf("expected a CompileError, got %v", err)
			}
		})
	}
}

func TestCompile_UnknownLanguage_ReturnsError(t *testing.T) {
	if _, err := Compile(context.Background(), "cobol", "DISPLAY 'HI'."); err == nil {
		t.Fatal("expected an error for an unsupported language")
	}
}

func TestLanguageOf_MapsSourceFilesBack(t *testing.T) {
	for _, lang := range Languages() {
		file, _ := SourceFile(lang)
		if got, ok := LanguageOf(file); !ok || got != lang {
			t.Fatalf("LanguageOf(%q) = %q, %v; want %q", file, got, ok, lang)
		}
	}
}
