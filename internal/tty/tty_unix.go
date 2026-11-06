//go:build unix

package tty

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal asks for the window size, which only a terminal can answer.
func isTerminal(f *os.File) bool {
	var ws struct{ rows, cols, xpixel, ypixel uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	return errno == 0
}
