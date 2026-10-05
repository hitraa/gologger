//go:build windows

package gologger

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const errorLockViolation = syscall.Errno(33)

var (
	lockFileEx   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	unlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

func lockFileExclusive(file *os.File) error {
	var overlapped syscall.Overlapped
	const exclusiveAndFailImmediately = 0x00000003
	result, _, callErr := lockFileEx.Call(
		file.Fd(),
		exclusiveAndFailImmediately,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		if callErr == errorLockViolation {
			return ErrFileInUse
		}
		return fmt.Errorf("LockFileEx: %w", callErr)
	}
	return nil
}

func unlockFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := unlockFileEx.Call(
		file.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		return fmt.Errorf("UnlockFileEx: %w", callErr)
	}
	return nil
}
