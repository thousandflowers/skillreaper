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

// Settings reads the terminal settings of f, so a test can tell whether a
// program gave its terminal back as it found it.
func Settings(f *os.File) (syscall.Termios, error) {
	var tio syscall.Termios
	err := ioctl(int(f.Fd()), ioctlGetTermios, unsafe.Pointer(&tio))
	return tio, err
}
