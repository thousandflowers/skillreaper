package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// stopSignal is the signal that asked the run in hand to stop, 0 for none.
var stopSignal atomic.Int32

// stopCode is the exit status of a run that was asked to stop: 128 plus the
// signal, as a shell reports it, so SIGTERM (143) reads apart from Ctrl-C (130).
func stopCode() int {
	if sig := stopSignal.Load(); sig != 0 {
		return 128 + int(sig)
	}
	return exitInterrupted
}

// lockState takes the lock every command that changes reaped/ shares. Prune,
// mute, restore and unmute each read a state file, change it and write it back,
// so any two running at once lose what one of them recorded.
func lockState(claudeDir string, wait time.Duration, stderr io.Writer) (*prune.Lock, bool) {
	l, err := prune.AcquireLockWait(claudeDir, wait)
	return l, lockTaken(err, stderr)
}

// lockTaken says why the lock was not taken, when it was not.
func lockTaken(err error, stderr io.Writer) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, prune.ErrLocked):
		fmt.Fprintln(stderr, "error: another reap is changing this directory; wait for it to finish")
	default:
		fmt.Fprintf(stderr, "error: could not take the lock: %v\n", err)
	}
	return false
}

// lockPoll is how often a wait for the lock looks again.
const lockPoll = 100 * time.Millisecond

// lockUnlessStopped waits for the shared lock as lockState does, and gives up
// the moment the run is asked to stop: a Ctrl-C must not sit behind another
// run for a quarter of a minute.
func lockUnlessStopped(claudeDir string, wait time.Duration, stopped func() bool) (*prune.Lock, error) {
	deadline := time.Now().Add(wait)
	for {
		l, err := prune.AcquireLock(claudeDir)
		if !errors.Is(err, prune.ErrLocked) || time.Now().After(deadline) || stopped() {
			return l, err
		}
		time.Sleep(lockPoll)
	}
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
	stopSignal.Store(0)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case s := <-c:
			if sig, ok := s.(syscall.Signal); ok {
				stopSignal.Store(int32(sig))
			}
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
	return hintRewriterAs(invoked, invoked, args)
}

// shownName is the command to print in a hint. Run through npx, neither name is
// on the PATH: `npx skillreaper` is what works there.
func shownName(invoked, wrapper string) string {
	if invoked == "skillreaper" && strings.Contains(filepath.ToSlash(wrapper), "/_npx/") {
		return "npx " + invoked
	}
	return invoked
}

// hintRewriterAs is hintRewriter with the name to print given apart from the
// name that was run.
func hintRewriterAs(invoked, shown string, args []string) func([]byte) []byte {
	if invoked != "skillreaper" || slices.ContainsFunc(args, asksJSON) {
		return nil
	}
	names := []string{`\[`}
	for cmd := range commandRules {
		if cmd != "" {
			names = append(names, regexp.QuoteMeta(cmd))
		}
	}
	// After the name: a subcommand or a "[", a flag, the word "to", or the
	// backquote that closes a bare `reap`.
	re := regexp.MustCompile(`(^|[\s\x60(])reap( (?:` + strings.Join(names, "|") + `|--|to )|\x60)`)
	with := []byte("${1}" + shown + "${2}")
	pad := []byte(strings.Repeat(" ", len(shown)-len("reap")))
	return func(p []byte) []byte {
		lines := bytes.SplitAfter(p, []byte("\n"))
		moved := false // the line before was rewritten, or carries on one that was
		for i, line := range lines {
			switch {
			case re.Match(line):
				lines[i], moved = re.ReplaceAll(line, with), true
			case moved && bytes.HasPrefix(line, []byte("   ")):
				// a description carried on below a rewritten command moves with it
				lines[i] = append(slices.Clone(pad), line...)
			default:
				moved = false
			}
		}
		return bytes.Join(lines, nil)
	}
}

// File lets terminal checks see through the tracker.
func (t *stdoutTracker) File() *os.File { return t.f }

// asksJSON reads one argument as the flag package does: --json, -json, or
// either with =true (or 1, t, ...).
func asksJSON(arg string) bool {
	name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
	if name != "json" || !strings.HasPrefix(arg, "-") {
		return false
	}
	if !hasValue {
		return true
	}
	on, err := strconv.ParseBool(value)
	return err == nil && on
}
