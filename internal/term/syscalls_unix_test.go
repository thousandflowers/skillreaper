//go:build darwin || linux

package term

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

// swap sets *v to to until the test ends.
func swap[T any](t *testing.T, v *T, to T) {
	t.Helper()
	old := *v
	*v = to
	t.Cleanup(func() { *v = old })
}

// selects answers the waits in turn: each step may act, and says what the
// wait returns; when the steps run out the real wait answers.
func selects(t *testing.T, steps ...func(nfd int, set *syscall.FdSet, limit *syscall.Timeval) (error, bool)) *int {
	t.Helper()
	calls := 0
	real := sysSelect
	swap(t, &sysSelect, func(nfd int, set *syscall.FdSet, limit *syscall.Timeval) error {
		calls++
		if calls <= len(steps) {
			if err, done := steps[calls-1](nfd, set, limit); done {
				return err
			}
		}
		return real(nfd, set, limit)
	})
	return &calls
}

// interrupted is a wait that a signal cut short.
func interrupted(int, *syscall.FdSet, *syscall.Timeval) (error, bool) { return syscall.EINTR, true }

// spurious is a wait that returns with nothing ready.
func spurious(_ int, set *syscall.FdSet, _ *syscall.Timeval) (error, bool) {
	*set = syscall.FdSet{}
	return nil, true
}

func failing(err error) func(int, *syscall.FdSet, *syscall.Timeval) (error, bool) {
	return func(int, *syscall.FdSet, *syscall.Timeval) (error, bool) { return err, true }
}

// typedOnPipe is a bare terminal reading a pipe that already holds s.
func typedOnPipe(t *testing.T, s string) *Terminal {
	t.Helper()
	in, w := pipeFDs(t)
	_, out := pipeFDs(t)
	if _, err := syscall.Write(w, []byte(s)); err != nil {
		t.Fatal(err)
	}
	return bareTerminal(t, in, out, 24, 80)
}

func TestOpenWhenTheSettingsCannotBeReadOrChanged(t *testing.T) {
	for _, c := range []struct {
		call uintptr
		want string
	}{
		{ioctlGetTermios, "the terminal's settings cannot be read"},
		{ioctlSetTermios, "the terminal's settings cannot be changed"},
	} {
		p := newPTY(t, 24, 80)
		onPTY(t, p, nil)
		before := p.termios()
		swap(t, &sysIoctl, func(fd int, req uintptr, arg unsafe.Pointer) error {
			if req == c.call {
				return syscall.EIO
			}
			return ioctl(fd, req, arg)
		})
		tm, err := Open()
		if tm != nil || err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("Open = %v, %v; want an error starting %q", tm, err, c.want)
		}
		if after := p.termios(); after.Lflag != before.Lflag {
			t.Errorf("a failed Open left the terminal changed: lflag %#x, was %#x", after.Lflag, before.Lflag)
		}
		sysIoctl = ioctl
		if tm, err := Open(); err != nil { // nothing left behind stops the next one
			t.Errorf("Open after a failed one: %v", err)
		} else {
			tm.Close()
		}
	}
}

func TestOpenOverSSHWaitsLongerForEscapes(t *testing.T) {
	tm := openOn(t, newPTY(t, 24, 80), map[string]string{"SSH_TTY": "/dev/ttys009"})
	if tm.dec.escWait != escWaitRemote {
		t.Errorf("over ssh an Esc waits %v, want %v", tm.dec.escWait, escWaitRemote)
	}
}

func TestReadKeyThroughWaitsThatSayNothing(t *testing.T) {
	tests := []struct {
		name  string
		steps func(tm *Terminal) []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool)
	}{
		{"a signal cuts the wait short", func(*Terminal) []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool) {
			return []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool){interrupted}
		}},
		{"the wait returns with nothing ready", func(*Terminal) []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool) {
			return []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool){spurious}
		}},
		{"the key is there but the second look finds nothing", func(*Terminal) []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool) {
			pass := func(int, *syscall.FdSet, *syscall.Timeval) (error, bool) { return nil, false }
			return []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool){pass, spurious}
		}},
		{"stopped during the wait, continued later", func(tm *Terminal) []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool) {
			stop := func(nfd int, set *syscall.FdSet, limit *syscall.Timeval) (error, bool) {
				err := selectRead(nfd, set, limit) // the key is ready, but by now the menu is stopped
				tm.mu.Lock()
				tm.suspended = true
				tm.mu.Unlock()
				return err, true
			}
			resume := func(int, *syscall.FdSet, *syscall.Timeval) (error, bool) {
				tm.mu.Lock()
				tm.suspended = false
				tm.mu.Unlock()
				_, _ = syscall.Write(tm.wakeW, []byte{0}) // fg wakes the wait
				return nil, false
			}
			return []func(int, *syscall.FdSet, *syscall.Timeval) (error, bool){stop, resume}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := typedOnPipe(t, "q")
			calls := selects(t, tt.steps(tm)...)
			if k := tm.ReadKey(-1); k != 'q' {
				t.Errorf("ReadKey = %v, want the key that was there", k)
			}
			if *calls < 2 {
				t.Errorf("the wait was asked %d times: the case under test never happened", *calls)
			}
		})
	}
}

func TestReadKeyWhenTheWaitFails(t *testing.T) {
	tm := typedOnPipe(t, "q")
	selects(t, failing(syscall.EBADF))
	if k := tm.ReadKey(-1); k != KeyEOF {
		t.Errorf("ReadKey after a failed wait = %v, want KeyEOF", k)
	}
}

func TestFDSourceThroughFailures(t *testing.T) {
	newSource := func(t *testing.T) *fdSource {
		r, w := pipeFDs(t)
		if _, err := syscall.Write(w, []byte("z")); err != nil {
			t.Fatal(err)
		}
		return &fdSource{fd: r}
	}
	t.Run("fill through a signal", func(t *testing.T) {
		src := newSource(t)
		selects(t, interrupted)
		if st := src.fill(patience); st != gotByte || string(src.buf) != "z" {
			t.Errorf("fill = %v %q, want the byte", st, src.buf)
		}
	})
	t.Run("fill on a wait that fails", func(t *testing.T) {
		src := newSource(t)
		selects(t, failing(syscall.EIO))
		if st := src.fill(patience); st != closed || !src.gone {
			t.Errorf("fill = %v, gone %v; want closed and gone", st, src.gone)
		}
	})
	t.Run("read on a look that fails", func(t *testing.T) {
		src := newSource(t)
		selects(t, failing(syscall.EIO))
		if st := src.read(); st != closed || !src.gone {
			t.Errorf("read = %v, gone %v; want closed and gone", st, src.gone)
		}
	})
	t.Run("read through a signal", func(t *testing.T) {
		src := newSource(t)
		cut := false
		swap(t, &sysRead, func(fd int, p []byte) (int, error) {
			if !cut {
				cut = true
				return -1, syscall.EINTR
			}
			return syscall.Read(fd, p)
		})
		if st := src.read(); st != gotByte || string(src.buf) != "z" || !cut {
			t.Errorf("read = %v %q (cut %v), want the byte after the signal", st, src.buf, cut)
		}
	})
}
