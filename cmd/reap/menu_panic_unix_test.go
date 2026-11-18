//go:build darwin || linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/menu"
	"github.com/thousandflowers/skillreaper/internal/prune"
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

// Ctrl-C while the apply waits behind another run's lock stops the apply too.
// The guard used to start after the wait, so the key was lost and everything
// the person had tried to stop was carried out once the lock came free.
func TestMenuApplyStoppedWhileWaitingForTheLockChangesNothing(t *testing.T) {
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	row := menuRow(t, reportRows(t), "skill", "copied")
	held, err := prune.AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		time.Sleep(200 * time.Millisecond)
		held.Release()
	}()

	err = applyOne(t, dir, row, menu.Prune)

	if err == nil || !strings.Contains(err.Error(), "interrupted before it was done") {
		t.Errorf("a stopped apply gave %v", err)
	}
	if !lexist(filepath.Join(dir, "skills", "copied", "SKILL.md")) {
		t.Error("the skill was pruned although the apply was stopped")
	}
}

// A plugin is switched off once, for all its rows. Stopped after that, its
// other rows are off too: they were reported "interrupted before it was done"
// and stayed in the list as if they were not.
func TestRowsOfAPluginAlreadyOffAreNotReportedAsInterrupted(t *testing.T) {
	home := deleteHome(t)
	rows := reportRows(t)
	bin := t.TempDir()
	// The fake claude does what Ctrl-C during the command does: it signals reap.
	script := "#!/bin/sh\nkill -INT $PPID\n/bin/sleep 0.3\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	row := menuRow(t, rows, "skill", "p:pl")
	other := menuRow(t, rows, "skill", "mine")

	out := (&menuApplier{dir: filepath.Join(home, ".claude")}).Apply([]menu.Pick{
		{Row: row, Verb: menu.Disable}, {Row: row, Verb: menu.Disable}, {Row: other, Verb: menu.Prune}})

	if out[0].Err != nil || out[1].Err != nil {
		t.Errorf("the plugin is off, yet its rows read: %v / %v", out[0].Err, out[1].Err)
	}
	if out[2].Err == nil || !strings.Contains(out[2].Err.Error(), "interrupted before it was done") {
		t.Errorf("what came after the stop: %v", out[2].Err)
	}
}

// Ctrl-C while another run holds the lock ends the wait at once. It was caught
// and then sat out the whole quarter of a minute.
func TestMenuApplyStopsWaitingForTheLockWhenAskedTo(t *testing.T) {
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	row := menuRow(t, reportRows(t), "skill", "copied")
	held, err := prune.AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release() // held to the end: only the stop can end the wait early
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()
	started := time.Now()

	err = applyOne(t, dir, row, menu.Prune)

	if waited := time.Since(started); waited > 5*time.Second {
		t.Errorf("the stop was held for %s", waited.Round(time.Second))
	}
	if err == nil || !strings.Contains(err.Error(), "interrupted before it was done") {
		t.Errorf("a stopped wait gave %v", err)
	}
}
