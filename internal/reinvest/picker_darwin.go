package reinvest

import "syscall"

// The ioctl requests that read and write a terminal's settings.
const (
	ioctlGetTermios = uintptr(syscall.TIOCGETA)
	ioctlSetTermios = uintptr(syscall.TIOCSETA)
)
