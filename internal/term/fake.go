package term

import (
	"math"
	"time"
)

type fakeEvent struct {
	key   Key
	mouse *Mouse
}

// Fake is a Screen with no terminal behind it: tests feed it keys and read back
// what was drawn. Its exported fields may be set before or between keys.
type Fake struct {
	// Gap is what KeyGap answers: by default a long time, as after a pause.
	Gap time.Duration
	// ShiftUnreadable makes this a terminal where Shift cannot be asked about.
	ShiftUnreadable bool
	// SKey is whether the s key counts as still down.
	SKey bool
	// LightBackground is what Light answers.
	LightBackground bool
	// ColorCount is what Colors answers: 256 unless set.
	ColorCount int
	// Beeps counts the bells rung, Flushes the calls to FlushInput.
	Beeps, Flushes int
	// MouseOn is whether mouse reports are on.
	MouseOn bool

	painting, shown *frame
	queue           []fakeEvent
	mouse           Mouse
	shift           bool
}

// NewFake is a screen of that size, with nothing drawn and no key waiting.
func NewFake(rows, cols int) *Fake {
	return &Fake{Gap: math.MaxInt64, ColorCount: 256, MouseOn: true, painting: newFrame(rows, cols), shown: newFrame(rows, cols)}
}

// Feed queues keys for ReadKey, in order.
func (f *Fake) Feed(keys ...Key) {
	for _, k := range keys {
		f.queue = append(f.queue, fakeEvent{key: k})
	}
}

// FeedMouse queues a KeyMouse that carries m.
func (f *Fake) FeedMouse(m Mouse) { f.queue = append(f.queue, fakeEvent{key: KeyMouse, mouse: &m}) }

// Lines is the last refreshed frame as plain text, one string per row, with
// nothing after a row's last character.
func (f *Fake) Lines() []string {
	lines := make([]string, f.shown.rows)
	for y := range lines {
		lines[y] = f.shown.text(y)
	}
	return lines
}

// StyleAt is the style of the cell at (y, x) in the last refreshed frame.
func (f *Fake) StyleAt(y, x int) Style {
	if y < 0 || y >= f.shown.rows || x < 0 || x >= f.shown.cols {
		return Style{}
	}
	return f.shown.cells[y*f.shown.cols+x].st
}

// SetShift says whether Shift counts as held from now on.
func (f *Fake) SetShift(held bool) { f.shift = held }

// Resize changes the window's size and empties it. It queues no key: feed
// KeyResize for a menu that waits to be told.
func (f *Fake) Resize(rows, cols int) {
	f.painting, f.shown = newFrame(rows, cols), newFrame(rows, cols)
}

func (f *Fake) Size() (rows, cols int)           { return f.painting.rows, f.painting.cols }
func (f *Fake) Erase()                           { f.painting.erase() }
func (f *Fake) Put(y, x int, s string, st Style) { f.painting.put(y, x, s, st) }

// Refresh makes what was painted the frame Lines and StyleAt answer about.
func (f *Fake) Refresh() {
	f.shown = &frame{rows: f.painting.rows, cols: f.painting.cols, cells: append([]cell(nil), f.painting.cells...)}
}

// ReadKey is the next key fed, or KeyEOF when none is left.
func (f *Fake) ReadKey(time.Duration) Key {
	if len(f.queue) == 0 {
		return KeyEOF
	}
	next := f.queue[0]
	f.queue = f.queue[1:]
	if next.mouse != nil {
		f.mouse = *next.mouse
	}
	return next.key
}

func (f *Fake) Mouse() Mouse          { return f.mouse }
func (f *Fake) KeyGap() time.Duration { return f.Gap }

// FlushInput keeps the keys that were fed: they stand for keys typed later.
func (f *Fake) FlushInput() { f.Flushes++ }
func (f *Fake) Beep()       { f.Beeps++ }

func (f *Fake) SetMouse(on bool) (was bool) {
	was, f.MouseOn = f.MouseOn, on
	return was
}

func (f *Fake) ShiftHeld() bool     { return f.shift && !f.ShiftUnreadable }
func (f *Fake) SHeld() bool         { return f.SKey }
func (f *Fake) ShiftReadable() bool { return !f.ShiftUnreadable }
func (f *Fake) Light() bool         { return f.LightBackground }
func (f *Fake) Colors() int         { return f.ColorCount }
