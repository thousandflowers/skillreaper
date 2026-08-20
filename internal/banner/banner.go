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

// Mark is the runtime wordmark, the same block lettering the README opens
// with. ASCII only, no color: it has to survive a pipe into a file, a paste
// into an issue, and a terminal with no color support.
const Mark = `S           K           I          L          L
######  #######   ###   ######  ####### ###### 
##   ## ##       ## ##  ##   ## ##      ##   ##
######  #####   ####### ######  #####   ###### 
##  ##  ##      ##   ## ##      ##      ##  ## 
##   ## ####### ##   ## ##      ####### ##   ##`

// MarkNarrow is the fallback for terminals too narrow for the block lettering,
// where the wide mark would wrap into noise. It is the mark skillreaper used
// everywhere before the block form led the report.
const MarkNarrow = `skillreaper
----------/`

// markWidth is how wide the block lettering is; a terminal narrower than this
// gets MarkNarrow instead of a wrapped mess.
const markWidth = 47

// minWidth is the narrowest terminal that still gets any mark at all. Below
// this a terminal is being used as a strip of status text, not read as a page.
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
	mark := Mark
	if widthKnown && cols < markWidth {
		mark = MarkNarrow
	}
	fmt.Fprintln(errOut, mark)
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
