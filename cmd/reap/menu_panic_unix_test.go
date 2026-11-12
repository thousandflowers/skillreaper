//go:build darwin || linux

package main

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/menu"
	"github.com/thousandflowers/skillreaper/internal/ptytest"
	"github.com/thousandflowers/skillreaper/internal/term"
)

func TestAMenuThatPanicsGivesTheTerminalBack(t *testing.T) {
	// Arrange: a terminal window, and a menu that crashes while it is open
	deleteHome(t)
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	go func() { // the window: takes what is drawn so the program never blocks on it
		defer close(drained)
		var buf [4096]byte
		for {
			if n, _ := syscall.Read(master, buf[:]); n <= 0 {
				return
			}
		}
	}()
	defer func() { // the far end goes last: closing it under a read would hang on macOS
		_ = slave.Close()
		select {
		case <-drained:
		case <-time.After(5 * time.Second):
			t.Error("the window never saw the terminal close")
		}
		_ = syscall.Close(master)
	}()
	before, err := ptytest.Settings(slave)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"TERM": "xterm-256color", "COLORFGBG": "15;0", "SSH_CONNECTION": "test", "SSH_TTY": ""} {
		t.Setenv(name, value)
	}
	stdin, stdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = slave, slave
	defer func() { os.Stdin, os.Stdout = stdin, stdout }()
	old := runMenu
	defer func() { runMenu = old }()
	runMenu = func(term.Screen, menu.Data, menu.Applier) { panic("a bug in the menu") }

	// Act
	var errOut bytes.Buffer
	func() {
		defer func() { _ = recover() }()
		run([]string{menuCommand}, bytes.NewReader(nil), &bytes.Buffer{}, &errOut)
	}()

	// Assert
	after, err := ptytest.Settings(slave)
	if err != nil {
		t.Fatal(err)
	}
	if after.Lflag != before.Lflag || after.Iflag != before.Iflag {
		t.Errorf("after a panic the terminal is left changed: lflag %#x (was %#x); stderr %q", after.Lflag, before.Lflag, errOut.String())
	}
}
