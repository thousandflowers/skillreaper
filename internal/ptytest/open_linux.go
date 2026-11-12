package ptytest

import (
	"os"
	"strconv"
	"syscall"
	"unsafe"
)

// Open opens a pseudo-terminal pair the way posix_openpt, unlockpt and
// ptsname do. The master is a raw descriptor, the slave a file.
func Open() (master int, slave *os.File, err error) {
	master, err = syscall.Open(ptmxPath, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, nil, err
	}
	var unlock, n int32
	if err := ioctl(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		_ = syscall.Close(master)
		return -1, nil, err
	}
	if err := ioctl(master, syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		_ = syscall.Close(master)
		return -1, nil, err
	}
	slave, err = openSlave("/dev/pts/"+strconv.Itoa(int(n)), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = syscall.Close(master)
		return -1, nil, err
	}
	return master, slave, nil
}

// termios calls, for tests that read a terminal's settings back.
const (
	ioctlGetTermios = syscall.TCGETS
	ioctlSetTermios = syscall.TCSETS
)
