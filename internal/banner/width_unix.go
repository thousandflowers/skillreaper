//go:build unix

package banner

import (
	"os"
	"syscall"
	"unsafe"
)

// terminalWidth asks the kernel for the window size. This is the only way to
// read it without adding a dependency; when the ioctl fails the width is
// reported as unknown rather than as zero, so the caller does not mistake an
// unanswered question for a narrow terminal.
func terminalWidth(f *os.File) (int, bool) {
	var ws struct {
		rows, cols, xpixel, ypixel uint16
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		f.Fd(),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 || ws.cols == 0 {
		return 0, false
	}
	return int(ws.cols), true
}
