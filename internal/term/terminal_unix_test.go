//go:build darwin || linux

package term

import (
	"math"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProblem(t *testing.T) {
	p := newPTY(t, 24, 80)
	onPTY(t, p, nil)
	if got := Problem(); got != "" {
		t.Errorf("a terminal with TERM set has a problem: %q", got)
	}
	for term, want := range map[string]string{"": "TERM is not set", "dumb": "TERM=dumb cannot place text on the screen"} {
		t.Setenv("TERM", term)
		if got := Problem(); got != want {
			t.Errorf("TERM=%q: Problem = %q, want %q", term, got, want)
		}
		if tm, err := Open(); tm != nil || err == nil || err.Error() != want {
			t.Errorf("TERM=%q: Open = %v, %v, want the problem as the error", term, tm, err)
		}
	}
	t.Setenv("TERM", "xterm")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	const notTerminal = "it needs an interactive terminal on both stdin and stdout"
	for name, swap := range map[string]func(){"stdin": func() { os.Stdin = r }, "stdout": func() { os.Stdout = w }} {
		os.Stdin, os.Stdout = p.slave, p.slave
		swap()
		if got := Problem(); got != notTerminal {
			t.Errorf("a pipe on %s: Problem = %q", name, got)
		}
	}
}

func TestOpenTakesTheTerminalAndCloseGivesItBack(t *testing.T) {
	p := newPTY(t, 30, 100)
	before := p.termios()
	if isRaw(before) {
		t.Fatal("a new pseudo-terminal starts raw")
	}
	p.typeIn("typed before launch\r")
	p.waitTyped()
	tm := openOn(t, p, nil)
	if got := p.termios(); !isRaw(got) {
		t.Errorf("after Open the terminal is not raw: lflag %#x iflag %#x", got.Lflag, got.Iflag)
	}
	if !p.waitFor(0, screenOn+modesOn) {
		t.Errorf("Open did not take the screen and the mouse: %q", p.output())
	}
	if rows, cols := tm.Size(); rows != 30 || cols != 100 {
		t.Errorf("Size = %d, %d, want the window's 30, 100", rows, cols)
	}
	if k := tm.ReadKey(50 * time.Millisecond); k != KeyNone {
		t.Errorf("what was typed before Open came through as %v", k)
	}
	if tm.Light() || tm.Colors() != 256 || tm.ShiftReadable() || tm.ShiftHeld() || tm.SHeld() {
		t.Errorf("light %v, colours %d, shift readable %v held %v, s %v", tm.Light(), tm.Colors(), tm.ShiftReadable(), tm.ShiftHeld(), tm.SHeld())
	}
	mark := len(p.output())
	tm.Close()
	if got := p.termios(); got != before {
		t.Errorf("Close left the terminal changed: lflag %#x, was %#x", got.Lflag, before.Lflag)
	}
	if !p.waitFor(mark, modesOff+screenOff) { // the window reads what Close wrote in its own time
		out := p.output()
		t.Errorf("Close did not give the screen and the mouse back: output ends %q", out[max(0, len(out)-40):])
	}
	out := p.output()
	tm.Close()
	time.Sleep(50 * time.Millisecond) // time for a second Close's bytes, were there any, to arrive
	if again := p.output(); again != out {
		t.Errorf("a second Close wrote %q", again[len(out):])
	}
}

func TestOpenAsksTheBackground(t *testing.T) {
	tests := []struct {
		name, colorfgbg, reply string
		light, asked           bool
	}{
		{"light answer", "", "\x1b]11;rgb:ffff/ffff/ffff\x1b\\", true, true},
		{"dark answer", "", "\x1b]11;rgb:0000/0000/0000\a", false, true},
		{"no answer", "", "", false, true},
		{"told by COLORFGBG", "0;15", "", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPTY(t, 24, 80)
			p.reply = tt.reply
			start := time.Now()
			tm := openOn(t, p, map[string]string{"COLORFGBG": tt.colorfgbg})
			if tm.Light() != tt.light {
				t.Errorf("Light = %v, want %v", tm.Light(), tt.light)
			}
			if asked := strings.Contains(p.output(), backgroundQuery); asked != tt.asked {
				t.Errorf("asked the terminal: %v, want %v", asked, tt.asked)
			}
			if took := time.Since(start); tt.reply == "" && tt.asked && took < backgroundWait {
				t.Errorf("gave up on the answer after %v, before %v", took, backgroundWait)
			}
			if k := tm.ReadKey(20 * time.Millisecond); k != KeyNone {
				t.Errorf("the answer came through as key %v", k)
			}
		})
	}
}

func TestATerminalThatHangsUpWhileAskedEndsInput(t *testing.T) {
	p := newPTY(t, 24, 80)
	p.hangUp = true
	tm := openOn(t, p, map[string]string{"COLORFGBG": ""})
	if tm.Light() {
		t.Error("a terminal that hung up reads as light")
	}
	for range 2 {
		if k := tm.ReadKey(time.Second); k != KeyEOF {
			t.Fatalf("ReadKey on a hung-up terminal = %v, want KeyEOF", k)
		}
	}
}

func TestReadKeyFromARealTerminal(t *testing.T) {
	p := newPTY(t, 24, 80)
	tm := openOn(t, p, nil)
	read := func(typed string) Key {
		t.Helper()
		p.typeIn(typed)
		return tm.ReadKey(patience)
	}
	if k := read("a"); k != 'a' {
		t.Errorf("a = %v", k)
	}
	if tm.KeyGap() != math.MaxInt64 {
		t.Errorf("the first key's gap = %v, want none before it", tm.KeyGap())
	}
	if k := read("\x1b[A"); k != KeyUp {
		t.Errorf("up = %v", k)
	}
	if gap := tm.KeyGap(); gap <= 0 || gap > patience {
		t.Errorf("the second key's gap = %v", gap)
	}
	if k := read("\x1b[<0;5;3M"); k != KeyMouse || tm.Mouse() != (Mouse{4, 2, "press"}) {
		t.Errorf("a click = %v, %+v", k, tm.Mouse())
	}
	if k := read("\x1b]11;rgb:0/0/0\ab"); k != 'b' {
		t.Errorf("a late reply then b = %v, want b alone", k)
	}
	split := make(chan Key)
	go func() { split <- tm.ReadKey(patience) }()
	p.typeIn("\x1b[")
	time.Sleep(20 * time.Millisecond) // an arrow split by a slow link
	p.typeIn("B")
	if k := <-split; k != KeyDown {
		t.Errorf("a split arrow = %v", k)
	}
	tm.Put(0, 0, "x", Style{})
	tm.Refresh()
	mark := len(p.output())
	if k := read("\x0c"); k != KeyResize {
		t.Errorf("Ctrl-L = %v, want a repaint", k)
	}
	tm.Refresh()
	if !p.waitFor(mark, "\x1b[2J\x1b[1;1Hx") {
		t.Errorf("after Ctrl-L the frame was not repainted whole: %q", p.output()[mark:])
	}
	start := time.Now()
	if k := tm.ReadKey(30 * time.Millisecond); k != KeyNone || time.Since(start) < 30*time.Millisecond {
		t.Errorf("an idle wait = %v after %v", k, time.Since(start))
	}
	tm.FlushInput()
	if k := tm.ReadKey(0); k != KeyNone {
		t.Errorf("an empty flushed input gave %v", k)
	}
}

func TestSignalsReachTheMenu(t *testing.T) {
	p := newPTY(t, 24, 80)
	tm := openOn(t, p, nil)
	p.setSize(40, 120)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	if k := tm.ReadKey(patience); k != KeyResize {
		t.Fatalf("after SIGWINCH ReadKey = %v", k)
	}
	if rows, cols := tm.Size(); rows != 40 || cols != 120 {
		t.Errorf("after SIGWINCH Size = %d, %d", rows, cols)
	}
	tm.SetMouse(false)
	mark := len(p.output())
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if k := tm.ReadKey(patience); k != KeyResize {
		t.Fatalf("after SIGCONT ReadKey = %v", k)
	}
	if !p.waitFor(mark, screenOn+modesOn+mouseOff) {
		t.Errorf("a continued menu did not take the screen back, mouse still off: %q", p.output()[mark:])
	}
	if !isRaw(p.termios()) {
		t.Error("a continued menu is not raw")
	}
}

// interruptedWhile runs wait on a thread of its own, and interrupts that thread
// with signals that only interrupt, while it waits.
func interruptedWhile(t *testing.T, wait func(), during func()) {
	t.Helper()
	tid, done := make(chan int), make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		tid <- threadID()
		wait()
	}()
	id := <-tid
	time.Sleep(20 * time.Millisecond)
	for range 5 {
		if err := interruptThread(id, syscall.SIGURG); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	during()
	select {
	case <-done:
	case <-time.After(patience):
		t.Fatal("the wait never ended")
	}
}

func TestReadKeySurvivesAStraySignal(t *testing.T) {
	p := newPTY(t, 24, 80)
	tm := openOn(t, p, nil)
	var k Key
	interruptedWhile(t, func() { k = tm.ReadKey(-1) }, func() { p.typeIn("q") })
	if k != 'q' {
		t.Errorf("a signal during the wait made ReadKey answer %v, want the key typed after it", k)
	}
}

func TestReadKeyOnADescriptorThatIsGone(t *testing.T) {
	_, out := pipeFDs(t)
	tm := bareTerminal(t, closedFD(t), out, 24, 80)
	if k := tm.ReadKey(time.Second); k != KeyEOF {
		t.Errorf("ReadKey on a closed descriptor = %v, want KeyEOF", k)
	}
}

func TestFDSource(t *testing.T) {
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	r, w := fds[0], fds[1]
	defer syscall.Close(r)
	src := &fdSource{fd: r}
	if _, err := syscall.Write(w, []byte("ab")); err != nil {
		t.Fatal(err)
	}
	for _, want := range "ab" {
		if b, st := src.next(time.Second); st != gotByte || rune(b) != want {
			t.Errorf("next = %q, %v, want %q", b, st, want)
		}
	}
	if st := src.read(); st != timedOut {
		t.Errorf("read with nothing there = %v, want timedOut", st)
	}
	start := time.Now()
	if _, st := src.next(20 * time.Millisecond); st != timedOut || time.Since(start) < 20*time.Millisecond {
		t.Errorf("next on an idle pipe = %v after %v", st, time.Since(start))
	}
	interruptedWhile(t, func() {
		if _, st := src.next(-1); st != gotByte {
			t.Errorf("a signal during the wait made next answer %v", st)
		}
	}, func() { _, _ = syscall.Write(w, []byte("c")) })
	_ = syscall.Close(w)
	if _, st := src.next(time.Second); st != closed || !src.gone {
		t.Errorf("next after the writer left = %v, gone %v", st, src.gone)
	}
	if st := src.fill(time.Second); st != closed {
		t.Errorf("a gone source filled: %v", st)
	}
	bad := closedFD(t)
	if st := (&fdSource{fd: bad}).fill(time.Second); st != closed {
		t.Errorf("fill on a closed descriptor = %v", st)
	}
	if st := (&fdSource{fd: bad}).read(); st != closed {
		t.Errorf("read on a closed descriptor = %v", st)
	}
}

func TestRefreshWritesOnlyWhatChanged(t *testing.T) {
	r, w := pipeFDs(t)
	tm := bareTerminal(t, -1, w, 3, 10)
	tm.Put(0, 0, "hi", Style{})
	tm.Refresh()
	first := drain(t, r)
	for _, want := range []string{syncOn, "\x1b[0m\x1b[2J", "\x1b[1;1Hhi\x1b[K", "\x1b[2;1H\x1b[K", "\x1b[3;1H\x1b[K", syncOff} {
		if !strings.Contains(first, want) {
			t.Errorf("the first frame lacks %q: %q", want, first)
		}
	}
	tm.Refresh()
	if got := drain(t, r); got != "" {
		t.Errorf("an unchanged frame wrote %q", got)
	}
	tm.Put(1, 0, "yo", Style{})
	tm.Refresh()
	if got, want := drain(t, r), syncOn+"\x1b[2;1Hyo\x1b[K"+syncOff; got != want {
		t.Errorf("one changed row wrote %q, want %q", got, want)
	}
	tm.narrow = true
	tm.Put(2, 0, "█ ok", Style{})
	tm.Erase()
	tm.Put(2, 0, "█ ok", Style{})
	tm.Refresh()
	if got := drain(t, r); !strings.Contains(got, "\x1b[3;1H# ok\x1b[K") || !strings.Contains(got, "\x1b[1;1H\x1b[K") {
		t.Errorf("a narrow locale's frame = %q", got)
	}
	tm.Beep()
	if was := tm.SetMouse(false); !was {
		t.Error("the mouse was on")
	}
	if was := tm.SetMouse(false); was {
		t.Error("the mouse was off")
	}
	tm.SetMouse(true)
	if got, want := drain(t, r), "\a"+mouseOff+mouseOn; got != want {
		t.Errorf("bell and mouse wrote %q, want %q", got, want)
	}
	quiet := func(state string) {
		t.Helper()
		tm.Put(0, 0, "changed", Style{})
		tm.Refresh()
		tm.Beep()
		tm.SetMouse(false)
		tm.SetMouse(true)
		if got := drain(t, r); got != "" {
			t.Errorf("a %s terminal wrote %q", state, got)
		}
	}
	tm.suspended = true
	quiet("suspended")
	tm.suspended, tm.closed = false, true
	tm.resume() // a continue that arrives as the menu closes
	quiet("closed")
}

func TestResizeMeasuresTheWindow(t *testing.T) {
	_, w := pipeFDs(t)
	tm := bareTerminal(t, -1, w, 3, 3)
	tm.full = false
	tm.resize()
	if rows, cols := tm.Size(); rows != fallbackRows || cols != fallbackCols || !tm.full {
		t.Errorf("a window that cannot be measured is %d×%d, full %v", rows, cols, tm.full)
	}
	p := newPTY(t, 0, 0)
	tm.out = p.fd
	tm.resize()
	if rows, cols := tm.Size(); rows != fallbackRows || cols != fallbackCols {
		t.Errorf("a window that says 0×0 is %d×%d", rows, cols)
	}
	p.setSize(50, 150)
	tm.resize()
	if rows, cols := tm.Size(); rows != 50 || cols != 150 {
		t.Errorf("a 50×150 window is %d×%d", rows, cols)
	}
	fr := tm.fr
	tm.Put(0, 0, "kept", Style{})
	tm.resize()
	if tm.fr != fr || fr.text(0) != "kept" {
		t.Error("a window measured again at the same size lost its frame")
	}
}

func TestWriteSendsEverything(t *testing.T) {
	t.Run("to a terminal left non-blocking", func(t *testing.T) {
		r, w := pipeFDs(t)
		if err := syscall.SetNonblock(w, true); err != nil {
			t.Fatal(err)
		}
		payload := strings.Repeat("0123456789abcdef", 1<<15) // more than a pipe holds
		got := make(chan string)
		go func() {
			time.Sleep(20 * time.Millisecond) // let the writer find it full
			var all []byte
			var buf [65536]byte
			for len(all) < len(payload) {
				n, err := syscall.Read(r, buf[:])
				if n <= 0 || err != nil {
					break
				}
				all = append(all, buf[:n]...)
			}
			got <- string(all)
		}()
		bareTerminal(t, -1, w, 1, 1).write(payload)
		if s := <-got; s != payload {
			t.Errorf("%d of %d bytes arrived", len(s), len(payload))
		}
	})
	t.Run("to a terminal that never takes it", func(t *testing.T) {
		r, w := pipeFDs(t)
		if err := syscall.SetNonblock(w, true); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			bareTerminal(t, -1, w, 1, 1).write(strings.Repeat("x", 1<<20))
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(patience):
			t.Fatal("write waits for ever on a terminal that takes nothing")
		}
		if got := drain(t, r); len(got) == 0 || len(got) >= 1<<20 {
			t.Errorf("%d bytes were taken", len(got))
		}
	})
	t.Run("to a descriptor that is gone", func(t *testing.T) {
		bareTerminal(t, -1, closedFD(t), 1, 1).write("lost") // returns at once
	})
}

func TestForegroundWhereNobodyOwnsTheTerminal(t *testing.T) {
	p := newPTY(t, 24, 80)
	if !bareTerminal(t, p.fd, p.fd, 1, 1).foreground() {
		t.Error("a terminal that is nobody's controlling one must count as ours")
	}
}

func TestHoldWithNoSignalReleasesQuietly(t *testing.T) {
	_, w := pipeFDs(t)
	tm := bareTerminal(t, -1, w, 1, 1)
	release := tm.Hold()
	if !tm.holding.Load() {
		t.Error("Hold did not hold")
	}
	release()
	if tm.holding.Load() || tm.held.Load() != nil {
		t.Error("release left the hold on")
	}
}
