package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/thousandflowers/skillreaper/internal/prune"
)

// stateLockWait is how long restore, mute and unmute wait behind another run.
// A prune of a thousand items takes under ten seconds.
const stateLockWait = 15 * time.Second

// exitInterrupted is the shell's code for a command stopped by Ctrl-C.
const exitInterrupted = 130

// lockState takes the lock every command that changes reaped/ shares. Prune,
// mute, restore and unmute each read a state file, change it and write it back,
// so any two running at once lose what one of them recorded.
func lockState(claudeDir string, wait time.Duration, stderr io.Writer) (*prune.Lock, bool) {
	l, err := prune.AcquireLockWait(claudeDir, wait)
	switch {
	case err == nil:
		return l, true
	case errors.Is(err, prune.ErrLocked):
		fmt.Fprintln(stderr, "error: another reap is changing this directory; wait for it to finish")
	default:
		fmt.Fprintf(stderr, "error: could not take the lock: %v\n", err)
	}
	return nil, false
}

// keepGoingWithoutReader makes a command that changes things outlive whoever reads
// its output: `reap prune --yes | head -1` must not stop halfway or before it
// starts. Writes then fail, and the exit status says so.
func keepGoingWithoutReader() { signal.Ignore(syscall.SIGPIPE) }

// applyGuard lets a run over many items finish the item in hand. The first
// Ctrl-C or SIGTERM asks the loop to stop between items, so it records what it
// did and releases the lock; a second one kills as usual.
func applyGuard() (stopped func() bool, release func()) {
	var hit atomic.Bool
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case <-c:
			hit.Store(true)
			signal.Stop(c)
		case <-done:
		}
	}()
	return hit.Load, func() {
		signal.Stop(c)
		close(done)
	}
}

// stdoutTracker remembers the first failed write, so a run whose output went
// nowhere does not exit 0. Renderers write through many fmt.Fprint calls and
// return nothing, so this is the one place that can see it.
type stdoutTracker struct {
	f       *os.File
	err     error
	rewrite func([]byte) []byte // nil leaves output as it is
}

func (t *stdoutTracker) Write(p []byte) (int, error) {
	out := p
	if t.rewrite != nil {
		out = t.rewrite(p)
	}
	_, err := t.f.Write(out)
	if err != nil {
		if t.err == nil {
			t.err = err
		}
		return 0, err
	}
	return len(p), nil
}

// invokedName is the command as the user typed it.
func invokedName(arg0 string) string {
	return strings.TrimSuffix(filepath.Base(arg0), ".exe")
}

// hintRewriter makes the commands the output suggests use the name that was
// actually run: an install may have skillreaper and no reap on the PATH, and
// `reap prune` is then a hint that fails. Every suggestion in the renderers names
// a subcommand, so that is what is matched. JSON is data, and stays as it is. Only
// the tool's other public name counts: a binary built under a scratch name (the
// render captures, go run) is no name to send anyone to.
func hintRewriter(invoked string, args []string) func([]byte) []byte {
	if invoked != "skillreaper" || slices.Contains(args, "--json") || slices.Contains(args, "-json") {
		return nil
	}
	names := []string{`\[`}
	for cmd := range commandRules {
		if cmd != "" {
			names = append(names, regexp.QuoteMeta(cmd))
		}
	}
	re := regexp.MustCompile(`(^|[\s\x60(])reap ((?:` + strings.Join(names, "|") + `))`)
	with := []byte("${1}" + invoked + " ${2}")
	return func(p []byte) []byte { return re.ReplaceAll(p, with) }
}

// File lets terminal checks see through the tracker.
func (t *stdoutTracker) File() *os.File { return t.f }
