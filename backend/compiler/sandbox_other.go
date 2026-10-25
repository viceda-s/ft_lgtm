//go:build !linux

package compiler

import "errors"

func execSandboxed(readOnly, readWrite, argv []string) error {
	return errors.New("the compiler sandbox needs Linux landlock")
}
