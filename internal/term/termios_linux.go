package term

import "syscall"

const (
	ioctlGetTermios = syscall.TCGETS
	ioctlSetTermios = syscall.TCSETS
)

// disableSuspendKeys has nothing to disable here: Linux has no delayed suspend key.
func disableSuspendKeys(*syscall.Termios) {}

func selectRead(nfd int, set *syscall.FdSet, limit *syscall.Timeval) error {
	_, err := syscall.Select(nfd, set, nil, nil, limit)
	return err
}
