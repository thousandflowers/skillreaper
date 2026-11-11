//go:build darwin || linux

package term

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/thousandflowers/skillreaper/internal/tty"
)

// Focus and the other mouse encodings may be left on by whatever ran before;
// drag needs 1002, wide windows 1006.
const (
	mouseOn   = "\x1b[?1002h\x1b[?1006h"
	mouseOff  = "\x1b[?1006l\x1b[?1002l\x1b[?1000l"
	modesOn   = "\x1b[?1004l\x1b[?1005l\x1b[?1015l" + mouseOn + "\x1b[?2004h"
	modesOff  = "\x1b[?2004l" + mouseOff
	screenOn  = "\x1b[?1049h\x1b[?25l\x1b[0m\x1b[2J" // the other screen, no cursor
	screenOff = "\x1b[0m\x1b[?25h\x1b[?1049l"
	syncOn    = "\x1b[?2026h" // a frame is shown whole or not at all, where the terminal knows how
	syncOff   = "\x1b[?2026l"

	backgroundWait = 100 * time.Millisecond
	fallbackRows   = 24
	fallbackCols   = 80
)

// Terminal is the real terminal, taken over for the menu until Close.
type Terminal struct {
	mu        sync.Mutex // the modes and what is written: the signal goroutine touches both
	in, out   int
	saved     syscall.Termios
	fr        *frame
	prev      []string // each row as last written, so a frame costs only what changed
	full      bool     // the next frame repaints everything
	src       *fdSource
	dec       decoder
	wakeR     int
	wakeW     int
	signals   chan os.Signal
	done      chan struct{}
	resized   atomic.Bool
	resumed   atomic.Bool
	mouse     bool
	suspended bool
	closed    bool
	lastKey   time.Time
	gap       time.Duration
	light     bool
	colors    int
	narrow    bool
	keyboard  *keyboardWatcher
	holding   atomic.Bool
	held      atomic.Pointer[os.Signal]
}

// Problem says why this cannot run as a full-screen menu, or "".
func Problem() string {
	if !tty.IsTerminal(os.Stdin) || !tty.IsTerminal(os.Stdout) {
		return "it needs an interactive terminal on both stdin and stdout"
	}
	switch name := os.Getenv("TERM"); name {
	case "":
		return "TERM is not set"
	case "dumb":
		return "TERM=dumb cannot place text on the screen"
	}
	return ""
}

// Open takes over the terminal: its own screen, keys one at a time, the mouse.
// The error is the reason alone, ready to follow "cannot open the menu: ".
// Close gives everything back, and must run before the program prints again.
func Open() (*Terminal, error) {
	if why := Problem(); why != "" {
		return nil, errors.New(why)
	}
	t := &Terminal{in: int(os.Stdin.Fd()), out: int(os.Stdout.Fd()), mouse: true, full: true,
		gap: math.MaxInt64, colors: colorCount(), narrow: Narrow(), done: make(chan struct{})}
	if err := ioctl(t.in, ioctlGetTermios, unsafe.Pointer(&t.saved)); err != nil {
		return nil, fmt.Errorf("the terminal's settings cannot be read: %w", err)
	}
	var pipe [2]int
	if err := syscall.Pipe(pipe[:]); err != nil {
		return nil, fmt.Errorf("no pipe to be woken through: %w", err)
	}
	t.wakeR, t.wakeW = pipe[0], pipe[1]
	for _, fd := range pipe {
		syscall.CloseOnExec(fd)
		_ = syscall.SetNonblock(fd, true) // ours alone: neither waking nor being woken may ever block
	}
	t.src = &fdSource{fd: t.in}
	t.dec = decoder{src: t.src, escWait: escWaitLocal}
	if os.Getenv("SSH_TTY") != "" {
		t.dec.escWait = escWaitRemote
	}
	// Before raw mode: a Ctrl-C in the first moments must find someone who puts
	// the terminal back.
	t.signals = make(chan os.Signal, 8)
	signal.Notify(t.signals, syscall.SIGWINCH, syscall.SIGCONT, syscall.SIGTSTP, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	go t.watchSignals()
	if err := t.setRaw(); err != nil {
		signal.Stop(t.signals)
		close(t.done)
		_ = syscall.Close(t.wakeR)
		_ = syscall.Close(t.wakeW)
		return nil, fmt.Errorf("the terminal's settings cannot be changed: %w", err)
	}
	t.light = t.askLight()
	t.keyboard = startKeyboardWatcher()
	t.write(screenOn + modesOn)
	t.resize()
	t.FlushInput() // keys typed before launch and replies to old queries are not commands
	return t, nil
}

// Close gives the terminal back as it was. It may be called more than once.
func (t *Terminal) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	signal.Stop(t.signals)
	close(t.done)
	t.keyboard.stop()
	if !t.suspended {
		t.write(modesOff)
		tty.FlushInput(os.Stdin) // what is left must not run in the shell
		t.write(screenOff)
		_ = ioctl(t.in, ioctlSetTermios, unsafe.Pointer(&t.saved))
	}
	_ = syscall.Close(t.wakeR)
	_ = syscall.Close(t.wakeW)
}

// setRaw makes keys arrive one at a time and unechoed. Signals stay with the
// terminal: Ctrl-C still interrupts.
func (t *Terminal) setRaw() error {
	raw := t.saved
	raw.Lflag &^= syscall.ICANON | syscall.ECHO | syscall.IEXTEN
	raw.Iflag &^= syscall.IXON // Ctrl-S freezes the screen until Ctrl-Q, which reads as a dead program
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	disableSuspendKeys(&raw)
	return ioctl(t.in, ioctlSetTermios, unsafe.Pointer(&raw))
}

// askLight is whether the colours chosen for a dark window would vanish here.
// Unknown reads as dark. A late answer is swallowed later as any other reply.
func (t *Terminal) askLight() bool {
	if light, ok := toldBackground(); ok {
		return light
	}
	t.write(backgroundQuery)
	if ready, _, err := waitReadable(t.in, -1, backgroundWait); err != nil || !ready {
		return false
	}
	var reply [64]byte
	n, _ := syscall.Read(t.in, reply[:])
	if n <= 0 {
		return false
	}
	light, _ := parseBackground(reply[:n])
	return light
}

func (t *Terminal) watchSignals() {
	for {
		select {
		case <-t.done:
			return
		case sig := <-t.signals:
			switch sig {
			case syscall.SIGWINCH:
				if !t.resized.Swap(true) {
					t.wake()
				}
			case syscall.SIGCONT:
				if !t.resumed.Swap(true) {
					t.wake()
				}
			case syscall.SIGTSTP:
				t.suspend()
			default:
				if t.holding.Load() {
					held := sig
					t.held.Store(&held)
					continue
				}
				t.exitOn(sig)
			}
		}
	}
}

// exitOn ends the program for a signal, the terminal given back first.
func (t *Terminal) exitOn(sig os.Signal) {
	t.Close() // the shell gets its terminal back
	code := 1
	if n, ok := sig.(syscall.Signal); ok {
		code = 128 + int(n)
	}
	os.Exit(code)
}

// Hold makes a signal that would end the program wait until release is called:
// whoever is halfway through changing files finishes the item in hand first.
func (t *Terminal) Hold() (release func()) {
	t.holding.Store(true)
	return func() {
		t.holding.Store(false)
		if sig := t.held.Swap(nil); sig != nil {
			t.exitOn(*sig)
		}
	}
}

// wake ends the wait ReadKey is in.
func (t *Terminal) wake() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		_, _ = syscall.Write(t.wakeW, []byte{0})
	}
}

// suspend is Ctrl-Z: the shell must find its own screen and settings.
func (t *Terminal) suspend() {
	t.mu.Lock()
	if !t.closed && !t.suspended {
		t.write(modesOff + screenOff)
		_ = ioctl(t.in, ioctlSetTermios, unsafe.Pointer(&t.saved))
		t.suspended = true
	}
	t.mu.Unlock()
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGSTOP)
}

func (t *Terminal) isSuspended() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.suspended
}

// resume is after fg: the terminal may have new modes and a new size.
func (t *Terminal) resume() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	if !t.foreground() {
		t.suspended = true // continued in the background: the terminal is someone else's until fg
		return
	}
	_ = t.setRaw()
	t.suspended = false
	modes := screenOn + modesOn
	if !t.mouse {
		modes += mouseOff
	}
	t.write(modes)
	t.resize()
}

// foreground says whether the terminal is this process's to read and set.
func (t *Terminal) foreground() bool {
	var group int32
	if err := ioctl(t.in, syscall.TIOCGPGRP, unsafe.Pointer(&group)); err != nil {
		return true
	}
	return int(group) == syscall.Getpgrp()
}

// resize takes the window's size, and with it an empty screen.
func (t *Terminal) resize() {
	var ws struct{ rows, cols, xpixel, ypixel uint16 }
	rows, cols := fallbackRows, fallbackCols
	if err := ioctl(t.out, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err == nil && ws.rows > 0 && ws.cols > 0 {
		rows, cols = int(ws.rows), int(ws.cols)
	}
	if t.fr == nil || t.fr.rows != rows || t.fr.cols != cols {
		t.fr = newFrame(rows, cols)
	}
	t.full = true
}

// write sends all of s, however many writes that takes.
func (t *Terminal) write(s string) {
	p := []byte(s)
	for stuck := 0; len(p) > 0 && stuck < 1000; {
		n, err := syscall.Write(t.out, p)
		if n > 0 {
			p, stuck = p[n:], 0
		}
		switch err {
		case nil, syscall.EINTR:
		case syscall.EAGAIN: // someone left the terminal non-blocking: it will take the rest in a moment
			stuck++
			time.Sleep(time.Millisecond)
		default:
			return
		}
	}
}

// Size is the window as last measured: a change arrives as KeyResize.
func (t *Terminal) Size() (rows, cols int) { return t.fr.rows, t.fr.cols }

// Erase empties the screen being painted.
func (t *Terminal) Erase() { t.fr.erase() }

// Put writes s at (y, x), in plain characters where the locale has no others.
func (t *Terminal) Put(y, x int, s string, st Style) {
	if t.narrow {
		s = Glyphs(s)
	}
	t.fr.put(y, x, s, st)
}

// Refresh shows what was painted, writing only the rows that changed.
func (t *Terminal) Refresh() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.suspended {
		return
	}
	var b strings.Builder
	b.WriteString(syncOn)
	full := t.full || len(t.prev) != t.fr.rows
	if full {
		b.WriteString("\x1b[0m\x1b[2J")
		t.prev = make([]string, t.fr.rows)
	}
	changed := full
	for y := range t.fr.rows {
		line := t.fr.line(y, t.colors)
		if full || line != t.prev[y] {
			fmt.Fprintf(&b, "\x1b[%d;1H%s", y+1, line)
			t.prev[y] = line
			changed = true
		}
	}
	t.full = false
	if changed {
		b.WriteString(syncOff)
		t.write(b.String())
	}
}

// ReadKey returns one whole key. A negative timeout waits for ever.
func (t *Terminal) ReadKey(timeout time.Duration) Key {
	var deadline time.Time
	if timeout >= 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		if t.resumed.Swap(false) {
			t.resume()
			return KeyResize
		}
		if t.resized.Swap(false) {
			t.mu.Lock()
			t.resize()
			t.mu.Unlock()
			return KeyResize
		}
		if t.src.gone {
			return KeyEOF
		}
		if len(t.src.buf) == 0 {
			left := time.Duration(-1)
			if timeout >= 0 {
				left = max(0, time.Until(deadline))
			}
			in := t.in
			if t.isSuspended() {
				in = -1 // until fg the keys are the shell's, and a read would block for ever
			}
			ready, woken, err := waitReadable(in, t.wakeR, left)
			switch {
			case err == syscall.EINTR:
				continue
			case err != nil:
				return KeyEOF
			case woken:
				var drain [64]byte
				_, _ = syscall.Read(t.wakeR, drain[:])
				continue
			case !ready:
				if timeout >= 0 && !time.Now().Before(deadline) {
					return KeyNone
				}
				continue
			}
			if t.isSuspended() {
				continue // stopped while waiting: what woke the wait was not a key for the menu
			}
			switch t.src.read() {
			case closed:
				return KeyEOF
			case timedOut:
				continue
			}
		}
		first, _ := t.src.next(0)
		k, ok := t.dec.decode(first)
		if !ok {
			continue
		}
		now := time.Now()
		if t.lastKey.IsZero() {
			t.gap = math.MaxInt64
		} else {
			t.gap = now.Sub(t.lastKey)
		}
		t.lastKey = now
		if k == keyCtrlL {
			t.mu.Lock()
			t.full = true
			t.mu.Unlock()
			return KeyResize
		}
		return k
	}
}

// Mouse is the last mouse report read.
func (t *Terminal) Mouse() Mouse { return t.dec.mouse }

// KeyGap is the time between the key just read and the one before it.
func (t *Terminal) KeyGap() time.Duration { return t.gap }

// FlushInput throws away what was typed before this moment.
func (t *Terminal) FlushInput() {
	t.src.buf = t.src.buf[:0]
	tty.FlushInput(os.Stdin)
}

// Beep rings the terminal's bell.
func (t *Terminal) Beep() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed && !t.suspended {
		t.write("\a")
	}
}

// SetMouse turns mouse reports on or off and returns how they were.
func (t *Terminal) SetMouse(on bool) (was bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	was, t.mouse = t.mouse, on
	if was != on && !t.closed && !t.suspended {
		if on {
			t.write(mouseOn)
		} else {
			t.write(mouseOff)
		}
	}
	return was
}

// ShiftHeld says whether Shift is down, or was a moment ago.
func (t *Terminal) ShiftHeld() bool { return t.keyboard.shiftHeld(time.Now()) }

// SHeld says whether the s key is down.
func (t *Terminal) SHeld() bool { return t.keyboard.sHeld() }

// ShiftReadable says whether the system can be asked about Shift here.
func (t *Terminal) ShiftReadable() bool { return t.keyboard.readable() }

// Light says whether the window's background is light.
func (t *Terminal) Light() bool { return t.light }

// Colors is how many colours can be drawn: 256, 8 or 0.
func (t *Terminal) Colors() int { return t.colors }

// fdSource reads the terminal's input, never waiting longer than it is told to.
type fdSource struct {
	fd   int
	buf  []byte
	gone bool
}

func (s *fdSource) next(wait time.Duration) (byte, readState) {
	if len(s.buf) == 0 {
		if st := s.fill(wait); st != gotByte {
			return 0, st
		}
	}
	b := s.buf[0]
	s.buf = s.buf[1:]
	return b, gotByte
}

func (s *fdSource) fill(wait time.Duration) readState {
	if s.gone {
		return closed
	}
	var deadline time.Time
	if wait >= 0 {
		deadline = time.Now().Add(wait)
	}
	for {
		left := time.Duration(-1)
		if wait >= 0 {
			left = max(0, time.Until(deadline))
		}
		ready, _, err := waitReadable(s.fd, -1, left)
		switch {
		case err == syscall.EINTR:
			continue
		case err != nil:
			s.gone = true
			return closed
		case !ready:
			return timedOut
		}
		if st := s.read(); st != timedOut {
			return st
		}
	}
}

// read takes what the terminal has, once a wait said it has something. A wait
// that a stop and a continue went through says so with nothing there, and the
// read would then block: it asks once more, and answers timedOut.
func (s *fdSource) read() readState {
	switch ready, _, err := waitReadable(s.fd, -1, 0); {
	case err != nil && err != syscall.EINTR:
		s.gone = true
		return closed
	case err != nil || !ready:
		return timedOut
	}
	var chunk [256]byte
	for {
		n, err := syscall.Read(s.fd, chunk[:])
		if err == syscall.EINTR {
			continue
		}
		if n <= 0 { // a terminal that is readable with nothing to read is gone
			s.gone = true
			return closed
		}
		s.buf = append(s.buf[:0], chunk[:n]...)
		return gotByte
	}
}

func ioctl(fd int, request uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), request, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// waitReadable waits until a or b has something to read. A negative descriptor
// is not waited on, and a negative timeout waits for ever.
func waitReadable(a, b int, timeout time.Duration) (aReady, bReady bool, err error) {
	var set syscall.FdSet
	for _, fd := range [...]int{a, b} {
		if fd >= 0 {
			fdAdd(&set, fd)
		}
	}
	top := max(a, b)
	var limit *syscall.Timeval
	if timeout >= 0 {
		tv := syscall.NsecToTimeval(int64(timeout))
		limit = &tv
	}
	if err := selectRead(top+1, &set, limit); err != nil {
		return false, false, err
	}
	return a >= 0 && fdHas(&set, a), b >= 0 && fdHas(&set, b), nil
}

func fdAdd(set *syscall.FdSet, fd int) {
	width := int(unsafe.Sizeof(set.Bits[0])) * 8
	set.Bits[fd/width] |= 1 << (uint(fd) % uint(width))
}

func fdHas(set *syscall.FdSet, fd int) bool {
	width := int(unsafe.Sizeof(set.Bits[0])) * 8
	return set.Bits[fd/width]&(1<<(uint(fd)%uint(width))) != 0
}
