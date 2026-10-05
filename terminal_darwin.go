//go:build darwin

package gologger

import (
	"os"
	"syscall"
	"unsafe"
)

func isTerminal(file *os.File) bool {
	var state syscall.Termios
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		file.Fd(),
		syscall.TIOCGETA,
		uintptr(unsafe.Pointer(&state)),
	)
	return errno == 0
}
