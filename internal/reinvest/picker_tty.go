//go:build darwin || linux

package reinvest

import (
	"errors"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// The picker's terminal, with the standard library only: raw mode through
// the termios ioctl (as internal/tty asks for the window size), and plain
// ANSI sequences for the screen. The prototype used curses.

const (
	enterScreen = "\x1b[?1049h\x1b[?25l" // the alternate screen, cursor hidden
	leaveScreen = "\x1b[?25h\x1b[?1049l"
	clearScreen = "\x1b[H\x1b[2J"
	resetAttr   = "\x1b[0m"
	readTenths  = 1 // a read gives up after this many tenths of a second, so signals are seen
)

var attrCodes = map[int]string{attrBold: "\x1b[1m", attrReverse: "\x1b[7m", attrDim: "\x1b[2m"}

func windowSize(f *os.File) (rows, cols int, err error) {
	var ws struct{ rows, cols, xpixel, ypixel uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0, 0, errno
	}
	return int(ws.rows), int(ws.cols), nil
}

// isTerminalFile asks for the window size, which only a terminal can answer.
func isTerminalFile(f *os.File) bool {
	_, _, err := windowSize(f)
	return err == nil
}

func termios(f *os.File, request uintptr, state *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), request, uintptr(unsafe.Pointer(state)))
	if errno != 0 {
		return errno
	}
	return nil
}

// screen is the terminal while the picker owns it.
type screen struct {
	in, out *os.File
	saved   syscall.Termios
}

// takeOver puts the keyboard in character-at-a-time mode with no echo.
// Ctrl-C stays a signal, as under curses.
func (s *screen) takeOver() error {
	raw := s.saved
	raw.Lflag &^= syscall.ICANON | syscall.ECHO
	raw.Cc[syscall.VMIN] = 0
	raw.Cc[syscall.VTIME] = readTenths
	if err := termios(s.in, ioctlSetTermios, &raw); err != nil {
		return err
	}
	_, err := s.out.WriteString(enterScreen)
	return err
}

// giveBack returns the terminal as it was found.
func (s *screen) giveBack() error {
	_, writeErr := s.out.WriteString(leaveScreen)
	return errors.Join(writeErr, termios(s.in, ioctlSetTermios, &s.saved))
}

func openScreen(in, out *os.File) (*screen, error) {
	s := &screen{in: in, out: out}
	if err := termios(in, ioctlGetTermios, &s.saved); err != nil {
		return nil, err
	}
	return s, s.takeOver()
}

// readByte waits a moment for one byte. ok is false when none came.
func (s *screen) readByte() (b byte, ok bool, err error) {
	var buf [1]byte
	n, err := syscall.Read(int(s.in.Fd()), buf[:])
	switch {
	case err == syscall.EINTR || err == syscall.EAGAIN:
		return 0, false, nil
	case err != nil:
		return 0, false, err
	}
	return buf[0], n == 1, nil
}

func isFinalByte(b byte) bool { return b >= 0x40 && b <= 0x7e }

// readKey gives the next key, or keyNone when nothing was pressed in time.
// An escape with nothing after it is the Escape key; with more, a sequence,
// of which only the arrows mean something here.
func (s *screen) readKey() (int, error) {
	b, ok, err := s.readByte()
	if err != nil || !ok {
		return keyNone, err
	}
	if b != keyEscape {
		return int(b), nil
	}
	b, ok, err = s.readByte()
	if err != nil {
		return keyNone, err
	}
	if !ok {
		return keyEscape, nil
	}
	if b != '[' && b != 'O' {
		return keyNone, nil
	}
	for {
		b, ok, err = s.readByte()
		if err != nil || !ok {
			return keyNone, err
		}
		if isFinalByte(b) {
			if key, known := arrowKeys[b]; known {
				return key, nil
			}
			return keyNone, nil
		}
	}
}

func (s *screen) paint(items []drawn) error {
	var b strings.Builder
	b.WriteString(clearScreen)
	for _, item := range items {
		b.WriteString("\x1b[" + strconv.Itoa(item.y+1) + ";" + strconv.Itoa(item.x+1) + "H")
		b.WriteString(attrCodes[item.attr])
		b.WriteString(item.text)
		b.WriteString(resetAttr)
	}
	_, err := s.out.WriteString(b.String())
	return err
}

func (s *screen) draw(swaps []swap, cur int) error {
	rows, cols, err := windowSize(s.out)
	if err != nil {
		return err
	}
	return s.paint(frame(swaps, cur, rows, cols))
}

// pickSwaps runs the picker on the person's terminal and returns the swaps
// they accepted.
func (a *app) pickSwaps(swaps []swap) (chosen []swap, err error) {
	in, okIn := fileOf(a.stdin)
	out, okOut := fileOf(a.term)
	if !okIn || !okOut {
		return nil, errors.New("the picker needs a terminal to read keys from and draw on")
	}
	scr, err := openScreen(in, out)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, scr.giveBack()) }()
	interrupts, resizes := make(chan os.Signal, 1), make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	signal.Notify(resizes, syscall.SIGWINCH)
	defer signal.Stop(interrupts)
	defer signal.Stop(resizes)
	return a.pickLoop(scr, swaps, interrupts, resizes)
}

// pickLoop draws, waits for a key or a signal, and acts on it, until the
// person is done.
func (a *app) pickLoop(scr *screen, swaps []swap, interrupts, resizes <-chan os.Signal) ([]swap, error) {
	cur, stale := 0, true
	for {
		if stale {
			if err := scr.draw(swaps, cur); err != nil {
				return nil, err
			}
			stale = false
		}
		select {
		case <-interrupts:
			return nil, errInterrupted
		case <-resizes:
			stale = true
			continue
		default:
		}
		key, err := scr.readKey()
		if err != nil {
			return nil, err
		}
		if key == keyNone {
			continue
		}
		stale = true
		if key == 'p' {
			if err := a.previewOutside(scr, swaps[cur].cand()); err != nil {
				return nil, err
			}
			continue
		}
		var done bool
		if swaps, cur, done = onKey(swaps, cur, key); done {
			return accepted(swaps), nil
		}
	}
}

// previewOutside hands the terminal to the pager and takes it back after.
func (a *app) previewOutside(scr *screen, c candidate) error {
	if err := scr.giveBack(); err != nil {
		return err
	}
	a.preview(c)
	return scr.takeOver()
}
