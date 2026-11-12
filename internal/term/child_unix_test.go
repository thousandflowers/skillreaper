//go:build darwin || linux

package term

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// childScenarios are what a child does on its terminal, each returning the
// child's exit code. They run in their own process because they end it, stop
// it, or take a terminal as their controlling one.
var childScenarios = map[string]func(report *os.File) int{
	// Opens and waits for keys until a signal ends it.
	"wait": func(report *os.File) int {
		tm, err := Open()
		if err != nil {
			fmt.Fprintf(report, "open failed: %v\n", err)
			return 4
		}
		fmt.Fprintln(report, "open")
		for tm.ReadKey(-1) != KeyEOF {
		}
		fmt.Fprintln(report, "eof")
		return 4
	},
	// Draws a character that needs UTF-8, then closes.
	"draw": func(report *os.File) int {
		tm, err := Open()
		if err != nil {
			fmt.Fprintf(report, "open failed: %v\n", err)
			return 4
		}
		tm.Put(0, 0, "█", Style{})
		tm.Refresh()
		tm.Close()
		fmt.Fprintln(report, "drawn")
		return 0
	},
	// Holds signals and never releases them.
	"hold-forever": func(report *os.File) int {
		tm, err := Open()
		if err != nil {
			fmt.Fprintf(report, "open failed: %v\n", err)
			return 4
		}
		_ = tm.Hold()
		fmt.Fprintln(report, "holding")
		time.Sleep(patience)
		fmt.Fprintln(report, "never stopped")
		return 0
	},
	// Holds signals, reads one key, and releases once a signal is held.
	"hold": func(report *os.File) int {
		tm, err := Open()
		if err != nil {
			fmt.Fprintf(report, "open failed: %v\n", err)
			return 4
		}
		release := tm.Hold()
		fmt.Fprintln(report, "holding")
		k := tm.ReadKey(patience)
		for end := time.Now().Add(patience); tm.held.Load() == nil && time.Now().Before(end); {
			time.Sleep(time.Millisecond)
		}
		fmt.Fprintf(report, "key %c, held %v\n", rune(k), tm.held.Load() != nil)
		release()
		fmt.Fprintln(report, "release returned")
		return 0
	},
	// Reads two keys across a stop and a continue.
	"stop": func(report *os.File) int {
		tm, err := Open()
		if err != nil {
			fmt.Fprintf(report, "open failed: %v\n", err)
			return 4
		}
		fmt.Fprintln(report, "open")
		first := tm.ReadKey(patience)
		var tio syscall.Termios
		_ = ioctl(tm.in, ioctlGetTermios, unsafe.Pointer(&tio))
		second := tm.ReadKey(patience)
		tm.Close()
		fmt.Fprintf(report, "first resize %v, raw %v, then %c\n", first == KeyResize, isRaw(tio), rune(second))
		return 0
	},
	// Panics with the terminal open and its Close deferred.
	"panic": func(report *os.File) int {
		tm, err := Open()
		if err != nil {
			fmt.Fprintf(report, "open failed: %v\n", err)
			return 4
		}
		defer tm.Close()
		fmt.Fprintln(report, "open")
		panic("boom")
	},
	// Is continued while another process group owns the terminal, as after bg.
	"background": func(report *os.File) int {
		tm, err := Open()
		if err != nil {
			fmt.Fprintf(report, "open failed: %v\n", err)
			return 4
		}
		owner, err := handTerminalAway()
		if err != nil {
			fmt.Fprintf(report, "cannot hand the terminal away: %v\n", err)
			return 4
		}
		defer owner()
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGCONT)
		first := tm.ReadKey(patience)
		suspended := tm.isSuspended()
		second := tm.ReadKey(20 * time.Millisecond)
		tm.Beep()
		tm.Close()
		fmt.Fprintf(report, "first resize %v, suspended %v, then none %v\n", first == KeyResize, suspended, second == KeyNone)
		return 0
	},
	// Opens from a background process group nobody can bring back.
	"orphan": func(report *os.File) int {
		owner, err := handTerminalAway()
		if err != nil {
			fmt.Fprintf(report, "cannot hand the terminal away: %v\n", err)
			return 4
		}
		defer owner()
		var before, after syscall.Termios
		_ = ioctl(0, ioctlGetTermios, unsafe.Pointer(&before))
		tm, err := Open()
		_ = ioctl(0, ioctlGetTermios, unsafe.Pointer(&after))
		fmt.Fprintf(report, "terminal %v, unchanged %v, %v\n", tm != nil, before == after, err)
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM) // nobody must still be catching it
		time.Sleep(patience / 2)
		return 0
	},
	// Opens with no descriptor left for the wake-up pipe.
	"no-pipe": func(report *os.File) int {
		lowest, err := syscall.Dup(0)
		if err != nil {
			fmt.Fprintf(report, "dup: %v\n", err)
			return 4
		}
		_ = syscall.Close(lowest)
		var limit syscall.Rlimit
		_ = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit)
		tight := limit
		tight.Cur = uint64(lowest)
		if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &tight); err != nil {
			fmt.Fprintf(report, "setrlimit: %v\n", err)
			return 4
		}
		var before, after syscall.Termios
		_ = ioctl(0, ioctlGetTermios, unsafe.Pointer(&before))
		tm, err := Open()
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit)
		_ = ioctl(0, ioctlGetTermios, unsafe.Pointer(&after))
		fmt.Fprintf(report, "terminal %v, unchanged %v, %v\n", tm != nil, before == after, err)
		return 0
	},
}

// handTerminalAway makes a new process group the terminal's foreground one, as
// a shell does for the job it runs, and returns how to end that group. The
// caller must be the terminal's session leader.
func handTerminalAway() (end func(), err error) {
	owner := exec.Command("sleep", "60")
	owner.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := owner.Start(); err != nil {
		return nil, err
	}
	end = func() {
		_ = owner.Process.Kill()
		_ = owner.Wait()
	}
	group := int32(owner.Process.Pid)
	if err := ioctl(0, syscall.TIOCSPGRP, unsafe.Pointer(&group)); err != nil {
		end()
		return nil, err
	}
	return end, nil
}

// controlling makes the child a session leader with the terminal as its own.
func controlling() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
}

func TestASignalGivesTheTerminalBack(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		t.Run(sig.String(), func(t *testing.T) {
			p := newPTY(t, 24, 80)
			before := p.termios()
			c := startChild(t, p, "wait", nil, nil)
			c.expect("open")
			if !isRaw(p.termios()) {
				t.Fatal("the child's terminal is not raw")
			}
			c.signal(sig)
			if code, killed := c.wait(); code != 128+int(sig) {
				t.Errorf("the child ended with %d (signal %v), want %d", code, killed, 128+int(sig))
			}
			if p.termios() != before {
				t.Error("the terminal was left raw")
			}
			if out := p.output(); !strings.HasSuffix(out, modesOff+screenOff) {
				t.Errorf("the screen was not given back: output ends %q", out[max(0, len(out)-40):])
			}
		})
	}
}

func TestAHeldSignalWaitsForRelease(t *testing.T) {
	p := newPTY(t, 24, 80)
	before := p.termios()
	c := startChild(t, p, "hold", nil, nil)
	c.expect("holding")
	c.signal(syscall.SIGTERM)
	p.typeIn("g")
	c.expect("key g, held true") // alive after the signal: it was held
	if code, _ := c.wait(); code != 128+int(syscall.SIGTERM) {
		t.Errorf("release ended the child with %d, want %d", code, 128+int(syscall.SIGTERM))
	}
	if p.termios() != before {
		t.Error("the terminal was left raw")
	}
}

func TestAHeldCtrlCStopsTheWorkButNotTheProgram(t *testing.T) {
	p := newPTY(t, 24, 80)
	c := startChild(t, p, "hold", nil, nil)
	c.expect("holding")
	c.signal(syscall.SIGINT)
	p.typeIn("g")
	c.expect("key g, held true")
	c.expect("release returned") // the menu goes on, to show what was done
	if code, _ := c.wait(); code != 0 {
		t.Errorf("a released Ctrl-C ended the child with %d, want it to carry on", code)
	}
}

func TestASecondSignalWhileHeldEndsTheProgramAtOnce(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		p := newPTY(t, 24, 80)
		before := p.termios()
		c := startChild(t, p, "hold-forever", nil, nil)
		c.expect("holding")
		c.signal(sig)
		time.Sleep(50 * time.Millisecond) // the first one is held
		c.signal(sig)
		if code, _ := c.wait(); code != 128+int(sig) {
			t.Errorf("%v twice while held ended the child with %d, want %d", sig, code, 128+int(sig))
		}
		if p.termios() != before {
			t.Errorf("%v twice left the terminal raw", sig)
		}
	}
}

func TestStopAndContinue(t *testing.T) {
	p := newPTY(t, 24, 80)
	before := p.termios()
	c := startChild(t, p, "stop", nil, nil)
	c.expect("open")
	mark := len(p.output())
	c.signal(syscall.SIGTSTP)
	c.waitStopped()
	if p.termios() != before {
		t.Error("a stopped menu left the terminal raw for the shell")
	}
	if !p.waitFor(mark, modesOff+screenOff) {
		t.Errorf("a stopped menu kept the screen: %q", p.output()[mark:])
	}
	mark = len(p.output())
	p.typeIn("k") // typed while stopped: the menu reads it once continued
	c.signal(syscall.SIGCONT)
	c.expect("first resize true, raw true, then k")
	if !strings.Contains(p.output()[mark:], screenOn+modesOn) {
		t.Errorf("a continued menu did not take the screen back: %q", p.output()[mark:])
	}
	if code, killed := c.wait(); code != 0 {
		t.Errorf("the child ended with %d (signal %v)", code, killed)
	}
	if p.termios() != before {
		t.Error("the terminal was left raw")
	}
}

func TestDeferredCloseRunsThroughAPanic(t *testing.T) {
	p := newPTY(t, 24, 80)
	before := p.termios()
	c := startChild(t, p, "panic", nil, nil)
	c.expect("open")
	if code, _ := c.wait(); code == 0 {
		t.Error("a panic ended the child cleanly")
	}
	if p.termios() != before {
		t.Error("the terminal was left raw")
	}
	out := p.output()
	if strings.LastIndex(out, screenOff) < strings.LastIndex(out, screenOn) {
		t.Error("the screen was not given back")
	}
}

func TestContinuedInTheBackground(t *testing.T) {
	p := newPTY(t, 24, 80)
	c := startChild(t, p, "background", nil, controlling())
	c.expect("first resize true, suspended true, then none true")
	if code, killed := c.wait(); code != 0 {
		t.Errorf("the child ended with %d (signal %v)", code, killed)
	}
	if out := p.output(); strings.Count(out, screenOn) != 1 || strings.Contains(out, "\a") {
		t.Errorf("a menu in the background drew on someone else's terminal: %q", out)
	}
}

func TestOpenFromAnOrphanedBackgroundGroup(t *testing.T) {
	p := newPTY(t, 24, 80)
	c := startChild(t, p, "orphan", nil, controlling())
	line := c.next()
	if want := "terminal false, unchanged true, the terminal's settings cannot be changed: "; !strings.HasPrefix(line, want) {
		t.Errorf("the child reported %q, want it to start %q", line, want)
	}
	if _, killed := c.wait(); killed != syscall.SIGTERM {
		t.Errorf("after a failed Open, SIGTERM was still caught (killed by %v)", killed)
	}
}

func TestOpenWithNoDescriptorLeft(t *testing.T) {
	p := newPTY(t, 24, 80)
	c := startChild(t, p, "no-pipe", nil, nil)
	line := c.next()
	if want := "terminal false, unchanged true, no pipe to be woken through: "; !strings.HasPrefix(line, want) {
		t.Errorf("the child reported %q, want it to start %q", line, want)
	}
	if code, _ := c.wait(); code != 0 {
		t.Errorf("the child ended with %d", code)
	}
}

func TestTheLocaleDecidesTheCharacters(t *testing.T) {
	for _, tt := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"no locale is ASCII", nil, "#"},
		{"a UTF-8 locale", map[string]string{"LC_ALL": "en_US.UTF-8"}, "█"},
		{"a Latin-1 locale", map[string]string{"LANG": "it_IT.ISO8859-1"}, "#"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := newPTY(t, 24, 80)
			c := startChild(t, p, "draw", tt.env, nil)
			c.expect("drawn")
			c.wait()
			if out := p.output(); !strings.Contains(out, "\x1b[1;1H"+tt.want+"\x1b[K") {
				t.Errorf("the block drew as %q, want %q", out, tt.want)
			}
		})
	}
}
