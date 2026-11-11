package term

import "syscall"

const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
	vdisable        = 0xff // _POSIX_VDISABLE
)

// disableSuspendKeys takes Ctrl-Y away from the terminal: it suspends on macOS,
// one stray modifier from the y that confirms.
func disableSuspendKeys(t *syscall.Termios) { t.Cc[syscall.VDSUSP] = vdisable }

func selectRead(nfd int, set *syscall.FdSet, limit *syscall.Timeval) error {
	return syscall.Select(nfd, set, nil, nil, limit)
}
