//go:build darwin || linux

package ptytest

import (
	"os"
	"syscall"
	"unsafe"
)

// ioctl runs one terminal control call on fd.
func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

// ptmxPath and openSlave are variables only so a test can make each step fail.
var (
	ptmxPath  = "/dev/ptmx"
	openSlave = os.OpenFile
)
