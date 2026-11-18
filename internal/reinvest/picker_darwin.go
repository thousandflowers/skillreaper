package reinvest

import "syscall"

// The ioctl requests that read and write a terminal's settings.
const (
	ioctlGetTermios = uintptr(syscall.TIOCGETA)
	ioctlSetTermios = uintptr(syscall.TIOCSETA)
	// ioctlSetTermiosFlush also drops the input not read yet.
	ioctlSetTermiosFlush = uintptr(syscall.TIOCSETAF)
)
