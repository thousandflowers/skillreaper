//go:build darwin || linux

package ptytest

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func TestOpen_gives_a_pair_that_carries_bytes_both_ways(t *testing.T) {
	master, slave, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(master)
	defer slave.Close()
	var tio syscall.Termios
	if err := ioctl(int(slave.Fd()), ioctlGetTermios, unsafe.Pointer(&tio)); err != nil {
		t.Fatalf("the slave is not a terminal: %v", err)
	}
	tio.Lflag &^= syscall.ECHO | syscall.ICANON // bytes as typed, no echo
	if err := ioctl(int(slave.Fd()), ioctlSetTermios, unsafe.Pointer(&tio)); err != nil {
		t.Fatal(err)
	}
	if _, err := syscall.Write(master, []byte("k")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if n, err := slave.Read(buf); err != nil || n != 1 || buf[0] != 'k' {
		t.Fatalf("slave read %q, %v", buf[:n], err)
	}
	if _, err := slave.Write([]byte("s")); err != nil {
		t.Fatal(err)
	}
	if n, err := syscall.Read(master, buf); err != nil || n != 1 || buf[0] != 's' {
		t.Fatalf("master read %q, %v", buf[:n], err)
	}
}

func TestIoctl_reports_the_error(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "plain")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var tio syscall.Termios
	if err := ioctl(int(f.Fd()), ioctlGetTermios, unsafe.Pointer(&tio)); err != syscall.ENOTTY {
		t.Errorf("a plain file gave %v, want ENOTTY", err)
	}
}

func TestOpen_fails_cleanly_at_every_step(t *testing.T) {
	plain := t.TempDir() + "/plain"
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		ptmx  string
		slave func(string, int, os.FileMode) (*os.File, error)
	}{
		{"no master device", t.TempDir() + "/none", os.OpenFile},
		{"a master that is no terminal", plain, os.OpenFile},
		{"a slave that will not open", "/dev/ptmx", func(string, int, os.FileMode) (*os.File, error) { return nil, syscall.EACCES }},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func(p string, o func(string, int, os.FileMode) (*os.File, error)) { ptmxPath, openSlave = p, o }(ptmxPath, openSlave)
			ptmxPath, openSlave = c.ptmx, c.slave
			master, slave, err := Open()
			if err == nil || master != -1 || slave != nil {
				t.Errorf("got %d, %v, %v; want -1, nil and an error", master, slave, err)
			}
		})
	}
}
