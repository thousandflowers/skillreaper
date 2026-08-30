//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package safepath

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadRegularFileWithinRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "input.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("FIFO unavailable: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ReadRegularFileWithin(root, fifo, 1024)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected FIFO to be rejected")
		}
	case <-time.After(time.Second):
		t.Fatal("reading a FIFO blocked instead of rejecting the non-regular file")
	}
}

func TestReadRegularFileWithinRejectsDevice(t *testing.T) {
	const device = "/dev/null"
	if _, err := os.Lstat(device); err != nil {
		t.Skipf("device unavailable: %v", err)
	}
	if _, err := ReadRegularFileWithin(filepath.Dir(device), device, 1024); err == nil {
		t.Fatal("expected device to be rejected")
	}
}
