package tty

import (
	"os"
	"syscall"
)

func flushInput(f *os.File) {
	const tcflsh, tciflush = 0x540B, 0 // TCFLSH, TCIFLUSH from asm-generic/ioctls.h
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), tcflsh, tciflush)
}
