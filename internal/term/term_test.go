package term

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// script is input that was all typed already: once it runs out, every wait
// times out, or the input is gone when shut.
type script struct {
	data []byte
	shut bool
}

func (s *script) next(time.Duration) (byte, readState) {
	if len(s.data) == 0 {
		if s.shut {
			return 0, closed
		}
		return 0, timedOut
	}
	b := s.data[0]
	s.data = s.data[1:]
	return b, gotByte
}

// keysFrom decodes everything in input, as ReadKey would key by key.
func keysFrom(input string) ([]Key, *decoder) {
	src := &script{data: []byte(input)}
	d := &decoder{src: src, escWait: escWaitLocal}
	var keys []Key
	for {
		first, st := src.next(0)
		if st != gotByte {
			return keys, d
		}
		if k, ok := d.decode(first); ok {
			keys = append(keys, k)
		}
	}
}

func TestDecodeKeys(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Key
	}{
		{"letters fold to lower case", "aQz", []Key{'a', 'q', 'z'}},
		{"J and K keep their case", "JKjk", []Key{'J', 'K', 'j', 'k'}},
		{"digits, space and tab pass", "1 \t", []Key{'1', ' ', KeyTab}},
		{"CR and LF are both enter", "\r\n", []Key{KeyEnter, KeyEnter}},
		{"arrows", "\x1b[A\x1b[B\x1b[C\x1b[D", []Key{KeyUp, KeyDown, KeyRight, KeyLeft}},
		{"application arrows", "\x1bOA\x1bOB\x1bOC\x1bOD", []Key{KeyUp, KeyDown, KeyRight, KeyLeft}},
		{"keypad enter", "\x1bOM", []Key{KeyEnter}},
		{"paging", "\x1b[5~\x1b[6~\x1b[H\x1b[F\x1b[1~\x1b[4~\x1bOH\x1bOF", []Key{KeyPgUp, KeyPgDn, KeyHome, KeyEnd, KeyHome, KeyEnd, KeyHome, KeyEnd}},
		{"shift arrows", "\x1b[1;2A\x1b[1;2B", []Key{KeyShiftUp, KeyShiftDown}},
		{"shift enter, three spellings", "\x1b[13;2u\x1b[27;2;13~\x1b\r", []Key{KeyShiftEnter, KeyShiftEnter, KeyShiftEnter}},
		{"kitty plain enter", "\x1b[13u", []Key{KeyEnter}},
		{"lone esc", "\x1b", []Key{KeyEsc}},
		{"esc cut off inside a sequence is no key", "\x1b[", nil},
		{"alt plus a key is no key", "\x1bqa", []Key{'a'}},
		{"unknown sequence is no key", "\x1b[99~a", []Key{'a'}},
		{"other shifted arrows are no key", "\x1b[1;2C\x1b[1;5A", nil},
		{"a paste is swallowed whole", "\x1b[200~rm -rf y\ny\x1b[201~a", []Key{'a'}},
		{"a paste never closed swallows the rest", "\x1b[200~yyy", nil},
		{"an OSC reply is swallowed, BEL ended", "\x1b]11;rgb:0000/0000/0000\ay", []Key{'y'}},
		{"an OSC reply is swallowed, ST ended", "\x1b]11;rgb:ffff/ffff/ffff\x1b\\y", []Key{'y'}},
		{"a DCS reply is swallowed", "\x1bP1$r0m\x1b\\n", []Key{'n'}},
		{"a two byte letter is one key", "é", []Key{KeyNonLatin}},
		{"a three byte letter is one key", "日y", []Key{KeyNonLatin, 'y'}},
		{"a four byte letter is one key", "😀q", []Key{KeyNonLatin, 'q'}},
		{"ctrl-l is left for the terminal to turn into a repaint", "\x0c", []Key{keyCtrlL}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := keysFrom(tt.input); !slices.Equal(got, tt.want) {
				t.Errorf("keys from %q = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDecodeMouse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Mouse
	}{
		{"press", "\x1b[<0;11;6M", Mouse{10, 5, "press"}},
		{"drag", "\x1b[<32;11;7M", Mouse{10, 6, "drag"}},
		{"release", "\x1b[<0;11;7m", Mouse{10, 6, "release"}},
		{"wheel up", "\x1b[<64;3;4M", Mouse{2, 3, "wheel_up"}},
		{"wheel down", "\x1b[<65;3;4M", Mouse{2, 3, "wheel_down"}},
		{"right button is no click", "\x1b[<2;3;4M", Mouse{2, 3, "other"}},
		{"ctrl+click is no click", "\x1b[<16;3;4M", Mouse{2, 3, "other"}},
		{"shift+wheel is no wheel", "\x1b[<68;3;4M", Mouse{2, 3, "other"}},
		{"motion with no button is a release", "\x1b[<35;3;4M", Mouse{2, 3, "release"}},
		{"wide window", "\x1b[<0;301;120M", Mouse{300, 119, "press"}},
		{"X10 press", "\x1b[M +&", Mouse{10, 5, "press"}},
		{"X10 release", "\x1b[M#+&", Mouse{10, 5, "release"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, d := keysFrom(tt.input)
			if !slices.Equal(keys, []Key{KeyMouse}) {
				t.Fatalf("keys from %q = %v, want one KeyMouse", tt.input, keys)
			}
			if d.mouse != tt.want {
				t.Errorf("mouse from %q = %+v, want %+v", tt.input, d.mouse, tt.want)
			}
		})
	}
	if keys, _ := keysFrom("\x1b[<0;;4M\x1b[<1;2M\x1b[<1;2;3;4ma"); !slices.Equal(keys, []Key{'a'}) {
		t.Errorf("a broken mouse report must be no key, got %v", keys)
	}
}

func TestSourceGone(t *testing.T) {
	src := &script{shut: true}
	if _, st := src.next(0); st != closed {
		t.Fatalf("a shut source must say closed, got %v", st)
	}
	d := &decoder{src: &script{data: []byte{}, shut: true}}
	if k, ok := d.decode(esc); !ok || k != KeyEsc {
		t.Errorf("an esc followed by the end of input = %v, %v, want a lone esc", k, ok)
	}
}

func TestTextHelpers(t *testing.T) {
	// The expected values are what experiments/menu-prototype/reap_proto.py answers.
	cutMid := []struct {
		s    string
		n    int
		want string
	}{
		{"abcdefghij", 7, "abc…hij"}, {"abcdefghij", 6, "ab…hij"}, {"日本語日本語", 7, "日…本語"}, {"abcdef", 2, "ab"},
		{"abc", 5, "abc"}, {"naïve-skill-name-long", 9, "naïv…long"}, {"a日b本c語d", 6, "a…c語d"}, {"abcdef", 0, ""}, {"abcdef", -3, ""},
	}
	for _, tt := range cutMid {
		if got := CutMid(tt.s, tt.n); got != tt.want {
			t.Errorf("CutMid(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
	cells := map[string]int{"": 0, "abc": 3, "日本語": 6, "éx̶": 2, "⚠ ✓ … ▸ █ €": 11, "😀": 2, "ｱ": 1, "Ａ": 2}
	for s, want := range cells {
		if got := Cells(s); got != want {
			t.Errorf("Cells(%q) = %d, want %d", s, got, want)
		}
	}
	if got := Cut("日本語", 5); got != "日本" {
		t.Errorf(`Cut("日本語", 5) = %q, want "日本"`, got)
	}
	if got := Cut("abc", -1); got != "" {
		t.Errorf(`Cut("abc", -1) = %q, want ""`, got)
	}
	pads := []struct {
		s    string
		n    int
		want string
	}{{"日本語", 5, "日本 "}, {"ab", 4, "ab  "}, {"abcdef", 3, "abc"}, {"ab", 0, ""}, {"ab", -2, ""}}
	for _, tt := range pads {
		if got := Pad(tt.s, tt.n); got != tt.want {
			t.Errorf("Pad(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
	if got, want := Visible("a\x1b[2Jb‮ć́́́d​e\nf"), "a�[2Jb�ć́d�e�f"; got != want {
		t.Errorf("Visible = %q, want %q", got, want)
	}
	if got := Visible("plain name-1.2"); got != "plain name-1.2" {
		t.Errorf("Visible changed a plain name: %q", got)
	}
	if got, want := Glyphs("⚠ █▸ 日x̶ €5"), "! #> ?x EUR5"; got != want {
		t.Errorf("Glyphs = %q, want %q", got, want)
	}
	for locale, utf8 := range map[string]bool{"it_IT.UTF-8": true, "en_US.utf8": true, "C": false, "POSIX": false, "it_IT.ISO8859-1": false} {
		if got := isUTF8Locale(locale); got != utf8 {
			t.Errorf("isUTF8Locale(%q) = %v, want %v", locale, got, utf8)
		}
	}
}

func TestFakeScreen(t *testing.T) {
	f := NewFake(4, 10)
	bold := Style{FG: Idx(196), Bold: true}
	f.Put(0, 0, "hello", Style{})
	f.Put(1, 2, "日本x", bold)
	f.Put(2, 6, "abcdefgh", Style{}) // clipped before the last column
	f.Put(3, 0, "s̶t̶", Style{})
	f.Put(-1, 0, "no", Style{})
	f.Put(4, 0, "no", Style{})
	f.Put(0, 10, "no", Style{})
	f.Put(0, -1, "no", Style{})
	if got := f.Lines(); !slices.Equal(got, []string{"", "", "", ""}) {
		t.Fatalf("nothing is shown before Refresh, got %q", got)
	}
	f.Refresh()
	want := []string{"hello", "  日本x", "      abc", "s̶t̶"}
	if got := f.Lines(); !slices.Equal(got, want) {
		t.Errorf("Lines = %q, want %q", got, want)
	}
	for x, st := range map[int]Style{1: {}, 2: bold, 3: bold, 6: bold, 7: {}} {
		if got := f.StyleAt(1, x); got != st {
			t.Errorf("StyleAt(1, %d) = %+v, want %+v", x, got, st)
		}
	}
	if got := f.StyleAt(9, 9); got != (Style{}) {
		t.Errorf("StyleAt outside the window = %+v", got)
	}
	f.Put(1, 3, "z", Style{}) // on the second half of 日: the whole character goes
	f.Refresh()
	if got := f.Lines()[1]; got != "   z本x" {
		t.Errorf("overwriting half a wide character left %q", got)
	}
	f.Erase()
	f.Refresh()
	if got := strings.Join(f.Lines(), ""); got != "" {
		t.Errorf("Erase left %q", got)
	}
	if rows, cols := f.Size(); rows != 4 || cols != 10 {
		t.Errorf("Size = %d, %d", rows, cols)
	}
	f.Resize(2, 5)
	if rows, cols := f.Size(); rows != 2 || cols != 5 || len(f.Lines()) != 2 {
		t.Errorf("after Resize, Size = %d, %d and %d lines", rows, cols, len(f.Lines()))
	}

	f.Feed('a', KeyDown)
	f.FeedMouse(Mouse{3, 1, "press"})
	f.FlushInput() // a question's flush must not eat the answer a test fed
	got := []Key{f.ReadKey(-1), f.ReadKey(0), f.ReadKey(time.Second), f.ReadKey(-1)}
	if !slices.Equal(got, []Key{'a', KeyDown, KeyMouse, KeyEOF}) {
		t.Errorf("keys = %v", got)
	}
	if f.Mouse() != (Mouse{3, 1, "press"}) {
		t.Errorf("Mouse = %+v", f.Mouse())
	}
	if f.KeyGap() < time.Hour {
		t.Errorf("a fed key must read as a fresh one, gap %v", f.KeyGap())
	}
	if was := f.SetMouse(false); !was || f.MouseOn {
		t.Errorf("SetMouse(false) = %v, MouseOn %v", was, f.MouseOn)
	}
	f.Beep()
	if f.Beeps != 1 || f.Colors() != 256 || f.Light() {
		t.Errorf("beeps %d, colors %d, light %v", f.Beeps, f.Colors(), f.Light())
	}
}

func TestApplyKey(t *testing.T) {
	mac, plain := NewFake(1, 1), NewFake(1, 1)
	plain.ShiftUnreadable = true
	if got := ApplyKeyName(mac); got != "shift+enter" {
		t.Errorf("where Shift is readable the footer promises %q", got)
	}
	if got := ApplyKeyName(plain); got != "x" {
		t.Errorf("where Shift is not readable the footer promises %q", got)
	}
	tests := []struct {
		name    string
		s       *Fake
		shift   bool
		k       Key
		pending bool
		want    bool
	}{
		{"shift+enter as its own key", mac, false, KeyShiftEnter, true, true},
		{"nothing to apply", mac, false, KeyShiftEnter, false, false},
		{"enter under a held shift", mac, true, KeyEnter, true, true},
		{"enter under a stuck shift with nothing to apply still opens", mac, true, KeyEnter, false, false},
		{"plain enter", mac, false, KeyEnter, true, false},
		{"x where shift is readable is just x", mac, false, 'x', true, false},
		{"x where shift cannot be read", plain, false, 'x', true, true},
		{"enter where shift cannot be read", plain, true, KeyEnter, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.s.SetShift(tt.shift)
			if got := IsApply(tt.s, tt.k, tt.pending); got != tt.want {
				t.Errorf("IsApply = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseBackground(t *testing.T) {
	tests := []struct {
		reply     string
		light, ok bool
	}{
		{"\x1b]11;rgb:ffff/ffff/ffff\a", true, true},
		{"\x1b]11;rgb:0000/0000/0000\x1b\\", false, true},
		{"\x1b]11;rgb:1e1e/1e1e/2e2e\a", false, true},
		{"\x1b]11;rgb:ff/ff/ff\a", true, true},
		{"\x1b]11;rgb:f/f/f\a", true, true},
		{"\x1b]11;rgb:8080/8080/8080\a", true, true},
		{"\x1b]11;rgb:7f7f/7f7f/7f7f\a", false, true},
		{"abc", false, false},
		{"", false, false},
		{"\x1b]11;rgb:fffffffffffffffff/0/0\a", false, false},
	}
	for _, tt := range tests {
		if light, ok := parseBackground([]byte(tt.reply)); light != tt.light || ok != tt.ok {
			t.Errorf("parseBackground(%q) = %v, %v, want %v, %v", tt.reply, light, ok, tt.light, tt.ok)
		}
	}
}

func TestToldBackgroundAndColors(t *testing.T) {
	for value, want := range map[string][2]bool{"15;0": {false, true}, "0;15": {true, true}, "0;default;7": {true, true}, "": {false, false}, "0;default": {false, false}} {
		t.Setenv("COLORFGBG", value)
		if light, ok := toldBackground(); light != want[0] || ok != want[1] {
			t.Errorf("COLORFGBG=%q: light %v, ok %v, want %v", value, light, ok, want)
		}
	}
	tests := []struct {
		term, colorterm, noColor string
		want                     int
	}{{"xterm-256color", "", "", 256}, {"xterm", "truecolor", "", 256}, {"xterm", "", "", 8}, {"xterm-256color", "", "1", 0}}
	for _, tt := range tests {
		t.Setenv("TERM", tt.term)
		t.Setenv("COLORTERM", tt.colorterm)
		t.Setenv("NO_COLOR", tt.noColor)
		if got := colorCount(); got != tt.want {
			t.Errorf("TERM=%q COLORTERM=%q NO_COLOR=%q: %d colours, want %d", tt.term, tt.colorterm, tt.noColor, got, tt.want)
		}
	}
}

func TestFrameLine(t *testing.T) {
	f := newFrame(2, 20)
	red := Style{FG: Idx(196), Bold: true}
	f.put(0, 0, "ab", Style{})
	f.put(0, 2, "cd", red)
	f.put(0, 4, "e", red)
	f.put(0, 6, "f   ", Style{Dim: true}) // trailing blanks cost nothing
	if got, want := f.line(0, 256), "ab\x1b[0;1;38;5;196mcde\x1b[0m \x1b[0;2mf\x1b[0m\x1b[K"; got != want {
		t.Errorf("line at 256 colours = %q, want %q", got, want)
	}
	if got, want := f.line(0, 8), "ab\x1b[0;1;31mcde\x1b[0m \x1b[0;2mf\x1b[0m\x1b[K"; got != want {
		t.Errorf("line at 8 colours = %q, want %q", got, want)
	}
	if got, want := f.line(0, 0), "ab\x1b[0;1mcde\x1b[0m \x1b[0;2mf\x1b[0m\x1b[K"; got != want {
		t.Errorf("line with no colour = %q, want %q", got, want)
	}
	if got := f.line(1, 256); got != "\x1b[K" {
		t.Errorf("an empty row = %q", got)
	}
	f.put(1, 0, "x  ", Style{BG: Idx(6)}) // a coloured blank is not nothing
	if got, want := f.line(1, 256), "\x1b[0;48;5;6mx  \x1b[0m\x1b[K"; got != want {
		t.Errorf("a highlighted row = %q, want %q", got, want)
	}
	f.put(1, 0, "a\x1b[2Jb\tc", Style{})
	if got := f.text(1); strings.ContainsAny(got, "\x1b\t") {
		t.Errorf("a control character reached the screen: %q", got)
	}
	for index, want := range map[int]int{1: 1, 9: 1, 174: 1, 180: 3, 114: 2, 28: 0, 232: 0, 255: 7, 21: 4, 51: 6, 201: 5, 231: 7} {
		if got := basicColor(index); got != want {
			t.Errorf("basicColor(%d) = %d, want %d", index, got, want)
		}
	}
}

func TestKeyboardWatcher(t *testing.T) {
	var none *keyboardWatcher
	if none.shiftHeld(time.Now()) || none.sHeld() || none.readable() {
		t.Error("no watcher must know nothing")
	}
	none.stop()
	start := time.Now()
	w := &keyboardWatcher{alive: true}
	if !w.readable() || w.shiftHeld(start) {
		t.Error("before the helper's first line: readable, and Shift not held")
	}
	w.feed("00", start)
	w.feed("0", start)       // what osascript prints as the script ends
	w.feed("garbage", start) // never a state
	if w.shiftHeld(start) || w.sHeld() {
		t.Error("nothing is held yet")
	}
	w.feed("11", start)
	if !w.shiftHeld(start) || !w.sHeld() {
		t.Error("both keys are down")
	}
	w.feed("00", start.Add(time.Second))
	if !w.shiftHeld(start.Add(time.Second + shiftGrace/2)) {
		t.Error("Shift must still count just after it is released")
	}
	if w.shiftHeld(start.Add(time.Second+shiftGrace)) || w.sHeld() {
		t.Error("Shift must stop counting once the grace is over")
	}
	w.feed("10", start.Add(2*time.Second))
	w.ended()
	if w.readable() || w.shiftHeld(start.Add(3*time.Second)) {
		t.Error("a helper that ended can say nothing more")
	}
}

var _ Screen = (*Fake)(nil)
var _ Screen = (*Terminal)(nil)
