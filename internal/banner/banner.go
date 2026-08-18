// Package banner renders skillreaper's runtime wordmark.
//
// The mark and every rule that suppresses it live here so the two call sites
// (the default report and the usage text) only have to ask. The mark goes to
// stderr, never stdout, so it can never contaminate output something else is
// parsing.
package banner

import (
	"fmt"
	"io"
	"os"
)

// Mark is the runtime wordmark: 11 columns, 2 lines, ASCII only, no color.
const Mark = `skillreaper
----------/`

// minWidth is the narrowest terminal that still gets the mark. Below this a
// terminal is being used as a strip of status text, not read as a page.
const minWidth = 20

// Options carries the flags that make a run undecorated: every
// machine-readable output format, plus the explicit opt-out.
type Options struct {
	JSON     bool
	Markdown bool
	Agent    bool
	NoBanner bool
}

// Detect reports whether w is an interactive terminal, how wide it is, and
// whether the width could be determined at all. It is a variable so tests can
// simulate a terminal; nothing in production reassigns it.
var Detect = detect

// Print writes the wordmark to errOut when stdOut is an interactive terminal
// and nothing about the run asks for undecorated output. stdOut is inspected
// but never written to.
func Print(errOut, stdOut io.Writer, o Options) {
	tty, cols, widthKnown := Detect(stdOut)
	if !allow(o, tty, cols, widthKnown, os.Getenv("NO_COLOR")) {
		return
	}
	fmt.Fprintln(errOut, Mark)
}

// allow is the whole decision, split out so it can be tested without a
// terminal. An unknown width never suppresses: not being able to measure a
// terminal is not evidence that it is narrow.
func allow(o Options, tty bool, cols int, widthKnown bool, noColor string) bool {
	switch {
	case o.NoBanner, o.JSON, o.Markdown, o.Agent:
		return false
	case noColor != "":
		return false
	case !tty:
		return false
	case widthKnown && cols < minWidth:
		return false
	}
	return true
}

// detect answers from the stream itself rather than from the arguments: a
// redirect, a pipe, and a capture all look identical on the command line and
// only differ here.
func detect(w io.Writer) (tty bool, cols int, widthKnown bool) {
	f, ok := w.(*os.File)
	if !ok {
		return false, 0, false
	}
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false, 0, false
	}
	cols, widthKnown = terminalWidth(f)
	return true, cols, widthKnown
}
