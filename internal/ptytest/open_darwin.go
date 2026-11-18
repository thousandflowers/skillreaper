package ptytest

import (
	"bytes"
	"os"
	"syscall"
	"unsafe"
)

// Open opens a pseudo-terminal pair the way posix_openpt, grantpt, unlockpt
// and ptsname do. The master is a raw descriptor, the slave a file.
func Open() (master int, slave *os.File, err error) {
	master, err = syscall.Open(ptmxPath, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, nil, err
	}
	var name [128]byte
	for _, step := range []struct {
		req uintptr
		arg unsafe.Pointer
	}{{syscall.TIOCPTYGRANT, nil}, {syscall.TIOCPTYUNLK, nil}, {syscall.TIOCPTYGNAME, unsafe.Pointer(&name)}} {
		if err := ioctl(master, step.req, step.arg); err != nil {
			_ = syscall.Close(master)
			return -1, nil, err
		}
	}
	path := string(name[:bytes.IndexByte(name[:], 0)])
	slave, err = openSlave(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = syscall.Close(master)
		return -1, nil, err
	}
	return master, slave, nil
}

// termios calls, for tests that read a terminal's settings back.
const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)
