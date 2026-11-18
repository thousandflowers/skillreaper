// Package tty answers whether a stream is a person's terminal. os.ModeCharDevice
// cannot: /dev/null is a character device too, so `reap >/dev/null` used to count
// as a terminal.
package tty

import (
	"io"
	"os"
)

// Filer is a writer standing in front of a file.
type Filer interface{ File() *os.File }

// FileOf returns the file behind a stream, if there is one.
func FileOf(s any) (*os.File, bool) {
	switch v := s.(type) {
	case *os.File:
		return v, v != nil
	case Filer:
		f := v.File()
		return f, f != nil
	}
	return nil, false
}

// IsTerminal reports whether s is a terminal.
func IsTerminal(s any) bool {
	f, ok := FileOf(s)
	return ok && isTerminal(f)
}

// FlushInput throws away what was typed before this moment, so a question is
// only answered by keys pressed after it was on the screen. Anything but a
// terminal keeps its input: a script's piped answer is not a stray key.
func FlushInput(r io.Reader) {
	if f, ok := FileOf(r); ok && isTerminal(f) {
		flushInput(f)
	}
}
