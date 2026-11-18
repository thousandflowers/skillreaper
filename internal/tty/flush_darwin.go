package tty

import (
	"os"
	"syscall"
	"unsafe"
)

func flushInput(f *os.File) {
	const fread = 1 // FREAD: the input queue only
	which := fread
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCFLUSH), uintptr(unsafe.Pointer(&which)))
}
