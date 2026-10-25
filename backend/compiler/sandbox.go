package compiler

import (
	"fmt"
	"os"
	"strings"
)

const sandboxArg = "__lgtm_sandbox"

func RunSandboxIfRequested() {
	if len(os.Args) < 2 || os.Args[1] != sandboxArg {
		return
	}
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: __lgtm_sandbox <read-only paths> <read-write paths> <program> [args...]")
		os.Exit(sandboxFailedExitCode)
	}
	err := execSandboxed(splitPaths(os.Args[2]), splitPaths(os.Args[3]), os.Args[4:])
	fmt.Fprintln(os.Stderr, err)
	os.Exit(sandboxFailedExitCode)
}

func splitPaths(list string) []string {
	return strings.FieldsFunc(list, func(r rune) bool { return r == ':' })
}
