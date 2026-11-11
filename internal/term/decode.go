package term

import (
	"strconv"
	"strings"
	"time"
)

const (
	esc = 27
	bel = 7

	escWaitLocal  = 200 * time.Millisecond
	escWaitRemote = 500 * time.Millisecond // an arrow split by a slow link must not read as Esc

	pasteStart = "200~"
	pasteEnd   = "\x1b[201~"
)

var csiKeys = map[string]Key{
	"A": KeyUp, "B": KeyDown, "C": KeyRight, "D": KeyLeft,
	"5~": KeyPgUp, "6~": KeyPgDn, "H": KeyHome, "F": KeyEnd, "1~": KeyHome, "4~": KeyEnd,
	"1;2A": KeyShiftUp, "1;2B": KeyShiftDown, "13;2u": KeyShiftEnter, "27;2;13~": KeyShiftEnter, "13u": KeyEnter,
}

var ss3Keys = map[byte]Key{
	'A': KeyUp, 'B': KeyDown, 'C': KeyRight, 'D': KeyLeft, 'M': KeyEnter, 'H': KeyHome, 'F': KeyEnd,
}

// stringOpeners start OSC, DCS, APC, PM and SOS: terminal replies, never keys.
const stringOpeners = "]P_^X"

type readState int

const (
	gotByte readState = iota
	timedOut
	closed // the input is gone for good
)

// byteSource hands out the terminal's input one byte at a time.
type byteSource interface {
	// next waits up to wait for a byte. A negative wait is for ever.
	next(wait time.Duration) (byte, readState)
}

// decoder turns bytes into whole keys, so that nothing depends on what terminfo
// knows about this terminal.
type decoder struct {
	src     byteSource
	escWait time.Duration
	mouse   Mouse
}

// get is the next byte of a sequence already begun.
func (d *decoder) get() (byte, bool) {
	b, st := d.src.next(d.escWait)
	return b, st == gotByte
}

// decode makes a key of first and whatever follows it. ok is false for a whole
// sequence that means nothing here.
func (d *decoder) decode(first byte) (k Key, ok bool) {
	switch {
	case first >= 0x80:
		return d.swallowUTF8(first), true
	case first == esc:
		return d.afterEsc()
	case first == '\r':
		return KeyEnter, true
	}
	return fold(Key(first)), true
}

func fold(k Key) Key {
	if k >= 'A' && k <= 'Z' && k != 'J' && k != 'K' {
		return k + 32
	}
	return k
}

// afterEsc reads what follows an Esc. A lone Esc comes back as Esc.
func (d *decoder) afterEsc() (Key, bool) {
	b, ok := d.get()
	switch {
	case !ok:
		return KeyEsc, true
	case b == '[':
		return d.csi()
	case b == 'O':
		final, ok := d.get()
		if !ok {
			return 0, false
		}
		k, known := ss3Keys[final]
		return k, known
	case strings.IndexByte(stringOpeners, b) >= 0:
		d.swallowString()
		return 0, false
	case b == '\r' || b == '\n':
		return KeyShiftEnter, true // ESC CR: what Terminal sends for Shift+Return when a profile binds it
	}
	return 0, false // Alt plus a key
}

func (d *decoder) csi() (Key, bool) {
	var body []byte
	for {
		b, ok := d.get()
		if !ok {
			return 0, false
		}
		body = append(body, b)
		if b >= 0x40 && b <= 0x7e {
			break
		}
	}
	text := string(body)
	switch {
	case text == "M": // X10 mouse: three raw bytes follow
		var raw [3]int
		for i := range raw {
			b, ok := d.get()
			if !ok {
				return 0, false
			}
			raw[i] = int(b)
		}
		return d.mouseKey(raw[0]-32, raw[1]-33, raw[2]-33, false), true
	case text[0] == '<': // SGR mouse
		parts := strings.Split(text[1:len(text)-1], ";")
		if len(parts) != 3 {
			return 0, false
		}
		var n [3]int
		for i, part := range parts {
			v, err := strconv.Atoi(part)
			if err != nil {
				return 0, false
			}
			n[i] = v
		}
		return d.mouseKey(n[0], n[1]-1, n[2]-1, text[len(text)-1] == 'm'), true
	case text == pasteStart:
		d.swallowPaste()
		return 0, false
	}
	k, known := csiKeys[text]
	return k, known
}

// mouseKey remembers one mouse report. Only an unmodified left button paints:
// wheel, side buttons and ctrl+click are not a click.
func (d *decoder) mouseKey(code, x, y int, released bool) Key {
	button := code & 0b11
	kind := "other"
	switch {
	case code&64 != 0 && code&(128|4|8|16) == 0 && button < 2:
		kind = "wheel_up"
		if button != 0 {
			kind = "wheel_down"
		}
	case released || (button == 3 && code&(64|128) == 0):
		kind = "release"
	case button == 0 && code&(4|8|16|64|128) == 0:
		kind = "press"
		if code&32 != 0 {
			kind = "drag"
		}
	}
	d.mouse = Mouse{X: x, Y: y, Kind: kind}
	return KeyMouse
}

func (d *decoder) swallowString() {
	for {
		b, ok := d.get()
		if !ok || b == bel {
			return
		}
		if b == esc {
			d.get() // the backslash of ST
			return
		}
	}
}

func (d *decoder) swallowPaste() {
	tail := ""
	for !strings.HasSuffix(tail, pasteEnd) {
		b, ok := d.get()
		if !ok {
			return
		}
		tail += string(rune(b))
		if len(tail) > len(pasteEnd) {
			tail = tail[len(tail)-len(pasteEnd):]
		}
	}
}

// swallowUTF8 reads the rest of one character, so that it reads as one key.
func (d *decoder) swallowUTF8(lead byte) Key {
	rest := 0
	switch {
	case lead >= 0xf0:
		rest = 3
	case lead >= 0xe0:
		rest = 2
	case lead >= 0xc0:
		rest = 1
	}
	for ; rest > 0; rest-- {
		d.get()
	}
	return KeyNonLatin
}
