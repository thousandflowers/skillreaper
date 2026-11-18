// Package term is what the interactive menu asks of the terminal: one whole key
// at a time, a screen it paints by cell, and a clean way in and out. It has no
// dependency outside the standard library and no cgo.
package term

import "time"

// Color is a colour of the xterm 256 palette, kept one above its index so the
// zero value means the terminal's own default.
type Color int16

// Idx is the Color for xterm palette index n.
func Idx(n int) Color { return Color(n + 1) }

// Style is how a run of text is drawn.
type Style struct {
	FG, BG             Color
	Bold, Dim, Reverse bool
}

// Key is one whole key press. Printable ASCII arrives as its own code, with
// upper case folded to lower except J and K, which stand in for shift+down and
// shift+up where the terminal cannot say otherwise.
type Key int32

const (
	KeyNone  Key = -1 // the wait ran out
	KeyTab   Key = 9
	KeyEnter Key = 10 // CR and LF both
	KeyEsc   Key = 27
)

// The keys with no character of their own sit above every Unicode code point.
const (
	KeyUp Key = 0x110000 + 1 + iota
	KeyDown
	KeyLeft
	KeyRight
	KeyPgUp
	KeyPgDn
	KeyHome
	KeyEnd
	KeyShiftUp
	KeyShiftDown
	KeyShiftEnter
	KeyResize   // the window changed, or a repaint was asked for
	KeyMouse    // Screen.Mouse says what happened
	KeyNonLatin // a key whose letter is outside ASCII: no command can answer it
	KeyEOF      // stdin is gone
)

const (
	keyCtrlL Key = 12
	applyKey Key = 'x' // applies with no modifier, where Shift cannot be read
)

// Mouse is one mouse report. Kind is "press", "drag", "release", "wheel_up",
// "wheel_down" or "other"; only an unmodified left button presses and drags.
type Mouse struct {
	X, Y int
	Kind string
}

// Screen is a terminal the menu can draw on and read keys from.
type Screen interface {
	Size() (rows, cols int)
	Erase()
	// Put writes s at row y, column x, clipped by cells before the last column.
	// A position outside the window is ignored.
	Put(y, x int, s string, st Style)
	Refresh()
	// ReadKey returns one whole key. A negative timeout waits for ever; a
	// timeout that runs out returns KeyNone.
	ReadKey(timeout time.Duration) Key
	// Mouse is the last mouse report read.
	Mouse() Mouse
	// KeyGap is the time between the key just read and the one before it: a
	// repeat or a paste leaves almost none.
	KeyGap() time.Duration
	// FlushInput throws away what was typed before this moment.
	FlushInput()
	Beep()
	// SetMouse turns mouse reports on or off and returns how they were. A
	// screen with no use for the mouse hands it back, so a drag selects text.
	SetMouse(on bool) (was bool)
	// ShiftHeld says whether Shift is down, where the system can be asked.
	ShiftHeld() bool
	// SHeld says whether the s key is still down, where the system can be asked.
	SHeld() bool
	// ShiftReadable says whether ShiftHeld can ever answer true.
	ShiftReadable() bool
	// Light says whether the window's background is light.
	Light() bool
	// Colors is how many colours can be drawn: 256, 8 or 0.
	Colors() int
}

// ApplyKeyName is the key the footer may promise for applying: never one this
// terminal cannot deliver.
func ApplyKeyName(s Screen) string {
	if s.ShiftReadable() {
		return "shift+enter"
	}
	return string(rune(applyKey))
}

// IsApply reports whether k applies the choices: Shift+Enter however it arrives,
// or the plain key where Shift cannot be read. Enter under a held Shift counts
// only with something to apply: a stuck modifier must not take away what Enter
// opens.
func IsApply(s Screen, k Key, pending bool) bool {
	return pending && (k == KeyShiftEnter || (k == applyKey && !s.ShiftReadable()) || (k == KeyEnter && s.ShiftHeld()))
}
