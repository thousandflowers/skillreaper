//go:build darwin || linux

package reinvest

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/thousandflowers/skillreaper/internal/ptytest"
)

// The picker on a real terminal: a pseudo-terminal whose far end the test
// types on and reads the screen from, as a person at a terminal would.

const ttyCovWait = 10 * time.Second // the longest any step may take before the test gives up

func ttyCovIoctl(fd, request, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, arg); errno != 0 {
		return errno
	}
	return nil
}

// ttyCovControl runs fn on the file's descriptor without taking it out of
// the poller, so a read blocked on it ends when the file is closed.
func ttyCovControl(f *os.File, fn func(fd uintptr) error) error {
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := raw.Control(func(fd uintptr) { inner = fn(fd) }); err != nil {
		return err
	}
	return inner
}

// ttyCovTerm is a pseudo-terminal: the app gets the slave, the test the
// master, where everything drawn is collected.
type ttyCovTerm struct {
	master, slave *os.File
	mu            sync.Mutex
	screen        bytes.Buffer
	done          chan struct{}
}

func ttyCovOpen(t *testing.T, rows, cols int) *ttyCovTerm {
	t.Helper()
	fd, slave, err := ptytest.Open()
	if err != nil {
		t.Fatalf("no pseudo-terminal: %v", err)
	}
	master := os.NewFile(uintptr(fd), "ptmx")
	pt := &ttyCovTerm{master: master, slave: slave, done: make(chan struct{})}
	pt.resize(t, rows, cols)
	go func() {
		defer close(pt.done)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			pt.mu.Lock()
			pt.screen.Write(buf[:n])
			pt.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = slave.Close()
		_ = master.Close()
		select {
		case <-pt.done:
		case <-time.After(ttyCovWait):
			t.Error("the screen reader did not stop")
		}
	})
	return pt
}

func (pt *ttyCovTerm) resize(t *testing.T, rows, cols int) {
	t.Helper()
	ws := struct{ rows, cols, xpixel, ypixel uint16 }{uint16(rows), uint16(cols), 0, 0}
	err := ttyCovControl(pt.master, func(fd uintptr) error {
		return ttyCovIoctl(fd, uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws)))
	})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
}

func (pt *ttyCovTerm) text() string {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	return pt.screen.String()
}

// waitFor waits until the screen shows what ok looks for.
func (pt *ttyCovTerm) waitFor(t *testing.T, what string, ok func(screen string) bool) string {
	t.Helper()
	deadline := time.Now().Add(ttyCovWait)
	for {
		screen := pt.text()
		if ok(screen) {
			return screen
		}
		if time.Now().After(deadline) {
			t.Fatalf("the screen never showed %s:\n%q", what, screen)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (pt *ttyCovTerm) frames() int { return strings.Count(pt.text(), clearScreen) }

// press types keys and waits for the picker to draw again.
func (pt *ttyCovTerm) press(t *testing.T, keys string) string {
	t.Helper()
	before := pt.frames()
	pt.typeKeys(t, keys)
	return pt.waitFor(t, "a new frame after "+keys, func(string) bool { return pt.frames() > before })
}

func (pt *ttyCovTerm) typeKeys(t *testing.T, keys string) {
	t.Helper()
	if _, err := pt.master.WriteString(keys); err != nil {
		t.Fatalf("typing %q: %v", keys, err)
	}
}

// ttyCovSettings reads the terminal's settings.
func ttyCovSettings(t *testing.T, f *os.File) syscall.Termios {
	t.Helper()
	var st syscall.Termios
	if err := termios(f, ioctlGetTermios, &st); err != nil {
		t.Fatalf("termios: %v", err)
	}
	return st
}

func ttyCovFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "not-a-terminal"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func ttyCovClosed(t *testing.T) *os.File {
	t.Helper()
	f := ttyCovFile(t)
	_ = f.Close()
	return f
}

func ttyCovSwaps() []swap {
	written := candidate{Kind: kindSkill, Name: "ship-release", Slug: "ship-release", Created: true, Doc: writtenDoc,
		Cost: intPtr(50), Gap: "releases", Why: "Cuts your releases."}
	alpha := candidate{Kind: kindSkill, Name: "alpha-notes", Slug: "alpha-notes", Created: true, Doc: "alpha", Cost: intPtr(30),
		Gap: "notes", Why: "Takes your notes."}
	beta := alpha
	beta.Name, beta.Slug, beta.Why = "beta-notes", "beta-notes", "Takes notes, the other way."
	return []swap{
		{Mute: []skill{{Name: "old-release", Tokens: 60}}, Out: 60, Cands: []candidate{written}},
		{Mute: []skill{{Name: "old-notes", Tokens: 40}}, Out: 40, Cands: []candidate{alpha, beta}},
	}
}

func ttyCovNames(swaps []swap) []string {
	out := make([]string, len(swaps))
	for i, s := range swaps {
		out[i] = s.cand().Name
	}
	return out
}

type ttyCovPicked struct {
	chosen []swap
	err    error
}

func ttyCovPick(a *app, swaps []swap) <-chan ttyCovPicked {
	done := make(chan ttyCovPicked, 1)
	go func() {
		chosen, err := a.pickSwaps(swaps)
		done <- ttyCovPicked{chosen, err}
	}()
	return done
}

func ttyCovResult(t *testing.T, done <-chan ttyCovPicked) ttyCovPicked {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-time.After(ttyCovWait):
		t.Fatal("the picker never closed")
	}
	return ttyCovPicked{}
}

func ttyCovFirstFrame(t *testing.T, pt *ttyCovTerm) string {
	t.Helper()
	return pt.waitFor(t, "the picker", func(s string) bool { return strings.Contains(s, "reinvest · swaps") })
}

func TestTTYCov_only_a_terminal_counts_as_one(t *testing.T) {
	pt := ttyCovOpen(t, 24, 100)
	rows, cols, err := windowSize(pt.slave)
	if err != nil || rows != 24 || cols != 100 {
		t.Errorf("a 24x100 terminal measures %dx%d (%v)", rows, cols, err)
	}
	if !isTerminalFile(pt.slave) || !isTerminal(pt.slave) {
		t.Error("a terminal was not taken for one")
	}
	file := ttyCovFile(t)
	if _, _, err := windowSize(file); err == nil {
		t.Error("a plain file has a window size")
	}
	if isTerminalFile(file) || isTerminal(file) {
		t.Error("a plain file was taken for a terminal")
	}
}

func TestTTYCov_the_picker_takes_the_terminal_and_gives_it_back_as_it_was(t *testing.T) {
	pt := ttyCovOpen(t, 24, 100)
	before := ttyCovSettings(t, pt.slave)
	if before.Lflag&syscall.ICANON == 0 || before.Lflag&syscall.ECHO == 0 {
		t.Fatalf("a new terminal is not line by line with echo: %#x", before.Lflag)
	}
	scr, err := openScreen(pt.slave, pt.slave)
	if err != nil {
		t.Fatal(err)
	}
	raw := ttyCovSettings(t, pt.slave)
	if raw.Lflag&(syscall.ICANON|syscall.ECHO) != 0 || raw.Cc[syscall.VMIN] != 0 || raw.Cc[syscall.VTIME] != readTenths {
		t.Errorf("taken over, the terminal is %#x min %d time %d", raw.Lflag, raw.Cc[syscall.VMIN], raw.Cc[syscall.VTIME])
	}
	if raw.Lflag&syscall.ISIG == 0 {
		t.Error("Ctrl-C no longer interrupts")
	}
	pt.waitFor(t, "the alternate screen", func(s string) bool { return strings.Contains(s, enterScreen) })
	if err := scr.giveBack(); err != nil {
		t.Fatal(err)
	}
	if after := ttyCovSettings(t, pt.slave); after != before {
		t.Errorf("the terminal was given back changed:\n%+v\n%+v", before, after)
	}
	pt.waitFor(t, "the screen left", func(s string) bool { return strings.HasSuffix(s, leaveScreen) })
}

func TestTTYCov_a_terminal_that_cannot_be_taken_says_why(t *testing.T) {
	pt := ttyCovOpen(t, 24, 100)
	if _, err := openScreen(ttyCovFile(t), pt.slave); !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("a file as the keyboard gave %v", err)
	}
	if err := (&screen{in: ttyCovFile(t), out: pt.slave}).takeOver(); !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("raw mode on a file gave %v", err)
	}
	scr := &screen{in: pt.slave, out: ttyCovClosed(t), saved: ttyCovSettings(t, pt.slave)}
	if err := scr.takeOver(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("a screen that cannot be drawn on gave %v", err)
	}
	if err := scr.giveBack(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("leaving a closed screen gave %v", err)
	}
	if got := ttyCovSettings(t, pt.slave); got != scr.saved {
		t.Error("the settings were not put back when the screen could not be left")
	}
	both := (&screen{in: ttyCovFile(t), out: ttyCovClosed(t)}).giveBack()
	if !errors.Is(both, os.ErrClosed) || !errors.Is(both, syscall.ENOTTY) {
		t.Errorf("both failures are not reported: %v", both)
	}
}

func TestTTYCov_keys_are_read_as_the_terminal_sends_them(t *testing.T) {
	pt := ttyCovOpen(t, 24, 100)
	scr, err := openScreen(pt.slave, pt.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer scr.giveBack()
	for _, tc := range []struct {
		typed string
		want  []int
	}{
		{"", []int{keyNone}}, // nothing typed: the read gives up in time
		{"q", []int{'q'}},
		{"\r", []int{'\n'}}, // the terminal still turns Return into a newline
		{"\x1b", []int{keyEscape}},
		{"\x1b[A\x1b[B\x1b[C\x1b[D", []int{keyUp, keyDown, keyRight, keyLeft}},
		{"\x1bOA\x1bOB", []int{keyUp, keyDown}},  // the arrows in application mode
		{"\x1b[1;5C", []int{keyRight}},           // Ctrl-right: parameters before the final byte
		{"\x1b[Z", []int{keyNone}},               // back-tab: a sequence that means nothing here
		{"\x1bxk", []int{keyNone, 'k'}},          // Alt-x is dropped, the next key is not
		{"\x1b[1;", []int{keyNone}},              // a sequence cut short
		{"\x1b[15~j", []int{keyNone, 'j'}},       // F5, then j
		{"\x1b[A" + "l", []int{keyUp, 'l'}},      // nothing is lost between keys
		{"\x1b\x1b[B", []int{keyNone, '[', 'B'}}, // an escape followed by another takes both
	} {
		pt.typeKeys(t, tc.typed)
		time.Sleep(20 * time.Millisecond)
		var got []int
		for range tc.want {
			key, err := scr.readKey()
			if err != nil {
				t.Fatalf("%q: %v", tc.typed, err)
			}
			got = append(got, key)
		}
		if !sameInts(got, tc.want) {
			t.Errorf("%q read as %v, want %v", tc.typed, got, tc.want)
		}
		if key, _ := scr.readKey(); key != keyNone {
			t.Errorf("%q left %d unread", tc.typed, key)
		}
	}
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ttyCovNonBlocking is a pipe's read end that never waits: a read with
// nothing in it fails with EAGAIN.
func ttyCovNonBlocking(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetNonblock(fds[0], true); err != nil {
		t.Fatal(err)
	}
	r, w := os.NewFile(uintptr(fds[0]), "keys"), os.NewFile(uintptr(fds[1]), "typed")
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return r, w
}

// ttyCovSocket is the read end of a socket whose peer sent typed, one byte
// per write, and went away with something unread.
func ttyCovSocket(kind int, typed string) (*os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, kind, 0)
	if err != nil {
		return nil, err
	}
	keyboard, peer := fds[0], fds[1]
	// a read that would wait gives EAGAIN instead, so a socket that does not fail cannot hang the test
	_ = syscall.SetsockoptTimeval(keyboard, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 1})
	for i := 0; i < len(typed); i++ { // a datagram socket reads one message at a time
		_, _ = syscall.Write(peer, []byte{typed[i]})
	}
	_, _ = syscall.Write(keyboard, []byte("unread"))
	_ = syscall.Close(peer)
	return os.NewFile(uintptr(keyboard), "keyboard"), nil
}

// ttyCovBrokenKeyboard gives what was typed, then fails. Which socket type
// fails a read after its data differs by system (Linux: a stream, macOS: a
// datagram), so each is tried on a probe first.
func ttyCovBrokenKeyboard(t *testing.T, typed string) *os.File {
	t.Helper()
	for _, kind := range []int{syscall.SOCK_STREAM, syscall.SOCK_DGRAM} {
		probe, err := ttyCovSocket(kind, typed)
		if err != nil {
			continue
		}
		fails := ttyCovFailsAfter(probe, len(typed))
		_ = probe.Close()
		if !fails {
			continue
		}
		f, err := ttyCovSocket(kind, typed)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	t.Skip("no socket type here fails a read after its data")
	return nil
}

// ttyCovFailsAfter reports whether n bytes read fine and the next read fails.
func ttyCovFailsAfter(f *os.File, n int) bool {
	var b [1]byte
	for i := 0; i < n; i++ {
		if got, err := syscall.Read(int(f.Fd()), b[:]); got != 1 || err != nil {
			return false
		}
	}
	_, err := syscall.Read(int(f.Fd()), b[:])
	return err != nil && err != syscall.EAGAIN && err != syscall.EINTR
}

func TestTTYCov_a_keyboard_that_fails_ends_the_read_with_the_error(t *testing.T) {
	r, w := ttyCovNonBlocking(t)
	scr := &screen{in: r}
	if key, err := scr.readKey(); key != keyNone || err != nil {
		t.Errorf("a read that would wait gave %d, %v", key, err)
	}
	if _, err := w.WriteString("h"); err != nil {
		t.Fatal(err)
	}
	if key, err := scr.readKey(); key != 'h' || err != nil {
		t.Errorf("a waiting key gave %d, %v", key, err)
	}
	if key, err := (&screen{in: ttyCovClosed(t)}).readKey(); key != keyNone || err == nil {
		t.Errorf("a closed keyboard gave %d, %v", key, err)
	}
	for _, typed := range []string{"\x1b", "\x1b[", "\x1bO1"} { // the failure comes in the middle of a key
		key, err := (&screen{in: ttyCovBrokenKeyboard(t, typed)}).readKey()
		if key != keyNone || !errors.Is(err, syscall.ECONNRESET) {
			t.Errorf("%q then a failure gave %d, %v", typed, key, err)
		}
	}
}

func TestTTYCov_each_piece_is_drawn_at_its_place_in_its_style(t *testing.T) {
	pt := ttyCovOpen(t, 24, 100)
	scr := &screen{in: pt.slave, out: pt.slave}
	items := []drawn{{0, 2, "title", attrBold}, {3, 5, "plain", attrPlain}, {4, 0, "chosen", attrReverse}, {5, 15, "why", attrDim}}
	if err := scr.paint(items); err != nil {
		t.Fatal(err)
	}
	want := clearScreen + "\x1b[1;3H\x1b[1mtitle" + resetAttr + "\x1b[4;6Hplain" + resetAttr +
		"\x1b[5;1H\x1b[7mchosen" + resetAttr + "\x1b[6;16H\x1b[2mwhy" + resetAttr
	pt.waitFor(t, "the painted pieces", func(s string) bool { return s == want })
	if err := (&screen{out: ttyCovClosed(t)}).paint(items); !errors.Is(err, os.ErrClosed) {
		t.Errorf("painting a closed screen gave %v", err)
	}
	if err := (&screen{out: ttyCovFile(t)}).draw(ttyCovSwaps(), 0); !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("drawing on a file gave %v", err)
	}
}

func TestTTYCov_the_person_picks_swaps_with_the_keys(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	before := ttyCovSettings(t, pt.slave)
	a := fakeApp(t)
	a.stdin, a.term = pt.slave, pt.slave
	var previewed []string
	a.runPager = func(argv []string, text string) error {
		// the window reads what the picker wrote in its own time
		pt.waitFor(t, "the picker leaving its screen before the pager", func(s string) bool { return strings.HasSuffix(s, leaveScreen) })
		var now syscall.Termios
		if err := termios(pt.slave, ioctlGetTermios, &now); err != nil || now != before {
			t.Errorf("the pager ran with the keyboard still in raw mode (%v)", err)
		}
		previewed = append(previewed, text)
		return nil
	}
	t.Setenv("PAGER", "less")
	swaps := ttyCovSwaps()
	done := ttyCovPick(a, swaps)
	first := ttyCovFirstFrame(t, pt)
	for _, part := range []string{"mute      old-release", "ship-release", "for releases", "Cuts your releases.", "0 accepted"} {
		if !strings.Contains(first, part) {
			t.Errorf("the first frame does not show %q:\n%q", part, first)
		}
	}
	if strings.Contains(first, "Takes your notes.") {
		t.Error("the reason is shown for a swap the cursor is not on")
	}
	screen := pt.press(t, "\r") // accept the first; the cursor moves to the second
	if last := screen[strings.LastIndex(screen, clearScreen):]; !strings.Contains(last, "✓") ||
		!strings.Contains(last, "Takes your notes.") || !strings.Contains(last, "1 accepted") {
		t.Errorf("after enter: %q", last)
	}
	screen = pt.press(t, "\x1b[C") // the other candidate for the second swap
	if last := screen[strings.LastIndex(screen, clearScreen):]; !strings.Contains(last, "◂ beta-notes ▸") || !strings.Contains(last, "2/2") {
		t.Errorf("after right: %q", last)
	}
	pt.press(t, "p")
	if len(previewed) != 1 || !strings.Contains(previewed[0], "beta-notes") {
		t.Errorf("the preview showed %q", previewed)
	}
	if raw := ttyCovSettings(t, pt.slave); raw.Lflag&(syscall.ICANON|syscall.ECHO) != 0 {
		t.Error("the picker did not take the keyboard back after the pager")
	}
	pt.press(t, "x") // a key the picker does not use changes nothing
	pt.press(t, "\r")
	pt.typeKeys(t, "q")
	got := ttyCovResult(t, done)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if names := ttyCovNames(got.chosen); !sameWords(names, []string{"ship-release", "beta-notes"}) {
		t.Errorf("chosen %v", names)
	}
	if after := ttyCovSettings(t, pt.slave); after != before {
		t.Error("the terminal was not given back as it was")
	}
	pt.waitFor(t, "the screen left", func(s string) bool { return strings.HasSuffix(s, leaveScreen) })
	if swaps[0].OK || swaps[1].Idx != 0 {
		t.Error("the picker changed the swaps it was given")
	}
}

func TestTTYCov_a_resize_redraws_at_the_new_size(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	a := fakeApp(t)
	a.stdin, a.term = pt.slave, pt.slave
	done := ttyCovPick(a, ttyCovSwaps())
	first := ttyCovFirstFrame(t, pt)
	if !strings.Contains(first, "m mute on/off · q done") {
		t.Fatalf("a wide screen cuts the title: %q", first)
	}
	before := pt.frames()
	pt.resize(t, 30, 30)
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	screen := pt.waitFor(t, "a frame after the resize", func(string) bool { return pt.frames() > before })
	last := screen[strings.LastIndex(screen, clearScreen):]
	if !strings.Contains(last, "reinvest · swaps") || strings.Contains(last, "q done") {
		t.Errorf("after the resize to 30 columns: %q", last)
	}
	pt.typeKeys(t, "\x1b") // Escape closes, as q does
	if got := ttyCovResult(t, done); got.err != nil || len(got.chosen) != 0 {
		t.Errorf("closed with nothing accepted: %v %v", ttyCovNames(got.chosen), got.err)
	}
}

func TestTTYCov_ctrl_c_stops_the_picker_and_gives_the_terminal_back(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	before := ttyCovSettings(t, pt.slave)
	a := fakeApp(t)
	a.stdin, a.term = pt.slave, pt.slave
	done := ttyCovPick(a, ttyCovSwaps())
	ttyCovFirstFrame(t, pt)
	pt.press(t, "\r")
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	got := ttyCovResult(t, done)
	if !errors.Is(got.err, errInterrupted) || got.chosen != nil {
		t.Errorf("Ctrl-C gave %v %v", ttyCovNames(got.chosen), got.err)
	}
	if ttyCovSettings(t, pt.slave) != before {
		t.Error("an interrupted picker left the terminal in raw mode")
	}
}

// A closed window (SIGHUP) or a kill (SIGTERM) ends the picker as Ctrl-C does:
// uncaught, they ended reap with the terminal still raw, on the other screen
// and with no cursor.
func TestTTYCov_a_kill_or_a_hangup_gives_the_terminal_back_too(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		pt := ttyCovOpen(t, 30, 160)
		before := ttyCovSettings(t, pt.slave)
		a := fakeApp(t)
		a.stdin, a.term = pt.slave, pt.slave
		done := ttyCovPick(a, ttyCovSwaps())
		ttyCovFirstFrame(t, pt)
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
		if got := ttyCovResult(t, done); !errors.Is(got.err, errInterrupted) || got.chosen != nil {
			t.Errorf("%v gave %v %v", sig, ttyCovNames(got.chosen), got.err)
		}
		if ttyCovSettings(t, pt.slave) != before {
			t.Errorf("a picker ended by %v left the terminal in raw mode", sig)
		}
	}
}

func TestTTYCov_the_picker_loop_acts_on_signals_before_keys(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	a := fakeApp(t)
	scr, err := openScreen(pt.slave, pt.slave)
	if err != nil {
		t.Fatal(err)
	}
	defer scr.giveBack()
	interrupts, resizes := make(chan os.Signal, 1), make(chan os.Signal, 1)
	resizes <- syscall.SIGWINCH
	pt.typeKeys(t, "q")
	chosen, err := a.pickLoop(scr, ttyCovSwaps(), interrupts, resizes)
	if err != nil || len(chosen) != 0 {
		t.Errorf("q gave %v %v", chosen, err)
	}
	pt.waitFor(t, "a frame for the resize", func(string) bool { return pt.frames() >= 2 })
	time.Sleep(50 * time.Millisecond)
	if n := pt.frames(); n != 2 {
		t.Errorf("a resize then q drew %d frames, want 2", n)
	}
	interrupts <- os.Interrupt
	pt.typeKeys(t, "\r")
	if _, err := a.pickLoop(scr, ttyCovSwaps(), interrupts, resizes); !errors.Is(err, errInterrupted) {
		t.Errorf("a pending interrupt gave %v", err)
	}
	if key, _ := scr.readKey(); key != '\n' {
		t.Error("the interrupt was not seen before the key")
	}
}

func TestTTYCov_the_picker_stops_when_its_terminal_fails(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	a := fakeApp(t)
	none := make(chan os.Signal)
	swaps := ttyCovSwaps()
	if _, err := a.pickLoop(&screen{in: pt.slave, out: ttyCovFile(t)}, swaps, none, none); !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("a screen it cannot measure gave %v", err)
	}
	if _, err := a.pickLoop(&screen{in: ttyCovClosed(t), out: pt.slave}, swaps, none, none); err == nil {
		t.Error("a keyboard that fails gave no error")
	}
	keys, typed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer keys.Close()
	defer typed.Close()
	if _, err := typed.WriteString("p"); err != nil {
		t.Fatal(err)
	}
	// p hands the keyboard back first: one that is no terminal cannot be, and the pager is not run
	if _, err := a.pickLoop(&screen{in: keys, out: pt.slave}, swaps, none, none); !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("handing back a keyboard that is no terminal gave %v", err)
	}
	gone, err := os.OpenFile(pt.slave.Name(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	scr, err := openScreen(pt.slave, gone)
	if err != nil {
		t.Fatal(err)
	}
	defer scr.giveBack()
	a.runPager = func([]string, string) error { return gone.Close() } // the screen goes away while the pager runs
	pt.typeKeys(t, "p")
	if _, err := a.pickLoop(scr, swaps, none, none); !errors.Is(err, os.ErrClosed) {
		t.Errorf("a screen gone during the pager gave %v", err)
	}
}

func TestTTYCov_the_picker_needs_a_terminal_on_both_sides(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	a := fakeApp(t)
	a.stdin, a.term = strings.NewReader(""), pt.slave
	if _, err := a.pickSwaps(ttyCovSwaps()); err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("no keyboard file gave %v", err)
	}
	a.stdin, a.term = pt.slave, &bytes.Buffer{}
	if _, err := a.pickSwaps(ttyCovSwaps()); err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Errorf("no screen file gave %v", err)
	}
	a.stdin, a.term = ttyCovFile(t), pt.slave
	if _, err := a.pickSwaps(ttyCovSwaps()); !errors.Is(err, syscall.ENOTTY) {
		t.Errorf("a file as the keyboard gave %v", err)
	}
	if strings.Contains(pt.text(), enterScreen) {
		t.Error("the screen was taken although the keyboard could not be")
	}
}

// Shrunk until no swap fits, the picker takes no key but the ones that leave:
// Enter used to accept a swap that was nowhere on the screen.
func TestTTYCov_a_window_too_small_accepts_nothing(t *testing.T) {
	pt := ttyCovOpen(t, 30, 160)
	a := fakeApp(t)
	a.stdin, a.term = pt.slave, pt.slave
	done := ttyCovPick(a, ttyCovSwaps())
	ttyCovFirstFrame(t, pt)
	before := pt.frames()
	pt.resize(t, 5, 60)
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	screen := pt.waitFor(t, "a frame after the resize", func(string) bool { return pt.frames() > before })
	if last := screen[strings.LastIndex(screen, clearScreen):]; !strings.Contains(last, "window too small") {
		t.Errorf("the small window shows %q", last)
	}
	pt.press(t, "\r") // would accept the first swap
	pt.typeKeys(t, "q")
	if got := ttyCovResult(t, done); got.err != nil || len(got.chosen) != 0 {
		t.Errorf("a swap was accepted unseen: %v %v", ttyCovNames(got.chosen), got.err)
	}
}
