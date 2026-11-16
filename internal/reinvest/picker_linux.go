package reinvest

import "syscall"

// The ioctl requests that read and write a terminal's settings.
const (
	ioctlGetTermios = uintptr(syscall.TCGETS)
	ioctlSetTermios = uintptr(syscall.TCSETS)
	// ioctlSetTermiosFlush also drops the input not read yet: TCSETSF, which the
	// syscall package does not name. It is TCSETS plus two on every Linux port.
	ioctlSetTermiosFlush = uintptr(syscall.TCSETS) + 2
)
