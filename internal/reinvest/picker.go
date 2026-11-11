package reinvest

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// The picker is one list. Each entry shows the skills to mute above and a candidate below:
//
//	up/down  previous / next swap      left/right  other candidates for this swap
//	enter    accept / undo the swap    q           done (then asks before changing anything)
//	p        read the candidate        m           mute nothing / mute again (keep the old skills)

// Keys the picker knows beyond plain characters.
const (
	keyEscape = 27
	keyUp     = 0x100 + iota
	keyDown
	keyLeft
	keyRight
	keyNone // nothing was pressed in time
)

// arrowKeys maps the last byte of a terminal's arrow sequence to its key.
var arrowKeys = map[byte]int{'A': keyUp, 'B': keyDown, 'C': keyRight, 'D': keyLeft}

var errInterrupted = errors.New("interrupted")

func withSwap(swaps []swap, at int, s swap) []swap {
	out := append([]swap{}, swaps...)
	out[at] = s
	return out
}

// onKey turns one key press into the new swaps, the new cursor and whether
// the picker closes. Kept free of terminal I/O so it can be tested.
func onKey(swaps []swap, cur int, key int) ([]swap, int, bool) {
	s := swaps[cur]
	last, next := len(swaps)-1, cur
	switch key {
	case keyDown, 'j':
		return swaps, min(cur+1, last), false
	case keyUp, 'k':
		return swaps, max(cur-1, 0), false
	case keyRight, 'l':
		s.Idx = (s.Idx + 1) % len(s.Cands)
	case keyLeft, 'h':
		s.Idx = (s.Idx - 1 + len(s.Cands)) % len(s.Cands)
	case '\n', '\r':
		s.OK = !s.OK
		if s.OK {
			next = min(cur+1, last)
		}
	case 'm':
		if len(s.Mute) > 0 || len(s.Held) > 0 {
			s = toggleMute(s)
		}
	case 'q', keyEscape:
		return swaps, cur, true
	default:
		return swaps, cur, false
	}
	return withSwap(swaps, cur, s), next, false
}

// accepted lists the accepted swaps; a candidate accepted twice is installed once.
func accepted(swaps []swap) []swap {
	var out []swap
	seen := map[string]bool{}
	for _, s := range swaps {
		if name := s.cand().Name; s.OK && !seen[name] {
			seen[name] = true
			out = append(out, s)
		}
	}
	return out
}

// Text attributes of a drawn line.
const (
	attrPlain = iota
	attrBold
	attrReverse
	attrDim
)

// drawn is one piece of text on the picker's screen.
type drawn struct {
	y, x int
	text string
	attr int
}

const (
	pickerTitle  = "reinvest · swaps   ↑↓ swap · ←→ candidate · enter accept · p read · m mute on/off · q done"
	verbWidth    = 9
	swapIndent   = 2
	candIndent   = 5
	detailIndent = 15
	rowsPerSwap  = 4
)

func alreadyPicked(swaps []swap, at int) bool {
	name := swaps[at].cand().Name
	for i, other := range swaps {
		if i != at && other.OK && other.cand().Name == name {
			return true
		}
	}
	return false
}

func padRight(text string, width int) string {
	return text + strings.Repeat(" ", max(0, width-runeLen(text)))
}

func candLine(s swap, taken bool) string {
	c := s.cand()
	pick := c.Name
	if len(s.Cands) > 1 {
		pick = "◂ " + c.Name + " ▸"
	}
	line := fmt.Sprintf("%s %s   %s · %s   %d/%d", padRight(verb(c), verbWidth), pick, weight(c.Cost, c.Installed),
		kindText(c), s.Idx+1, len(s.Cands))
	if taken {
		line += "   (already picked above)"
	}
	return line
}

// totals is the picker's last line: what is accepted and what it adds up to.
func totals(swaps []swap) string {
	chosen, freed, cost, unknown := 0, 0, 0, false
	for _, s := range swaps {
		if !s.OK {
			continue
		}
		chosen++
		freed += s.Out
		if s.cand().Cost == nil {
			unknown = true
		}
		cost += s.cand().costOrZero()
	}
	net := fmt.Sprintf("%+d tok/session", cost-freed)
	if unknown {
		net = "unknown (an MCP weight is unknown)"
	}
	return fmt.Sprintf("%d accepted · −%d muted · net %s", chosen, freed, net)
}

// frame lays the picker out for a screen of the given size. Text that does
// not fit is cut; rows below the screen are left out.
func frame(swaps []swap, cur, height, width int) []drawn {
	var out []drawn
	put := func(y, x int, text string, attr int) {
		room := width - x - 1
		if y < 0 || y >= height-1 || room <= 0 {
			return
		}
		out = append(out, drawn{y, x, firstRunes(text, room), attr})
	}
	put(0, swapIndent, pickerTitle, attrBold)
	y := 2
	for i, s := range swaps {
		selected := i == cur
		mark, muteAttr, candAttr := " ", attrPlain, attrPlain
		if s.OK {
			mark = "✓"
		}
		if selected {
			muteAttr, candAttr = attrReverse, attrBold
		}
		put(y, swapIndent, mark+"  mute      "+muteText(s), muteAttr)
		put(y+1, candIndent, candLine(s, alreadyPicked(swaps, i)), candAttr)
		put(y+2, detailIndent, "for "+s.cand().Gap, attrPlain)
		if selected {
			put(y+3, detailIndent, s.cand().Why, attrDim)
			y++
		}
		y += rowsPerSwap
	}
	put(height-2, swapIndent, totals(swaps), attrBold)
	return out
}

var (
	errNoEscaped = errors.New("no escaped character")
	errNoClosing = errors.New("no closing quotation")
)

// shellWords gathers the words of a command line as it is read.
type shellWords struct {
	words  []string
	word   strings.Builder
	inWord bool
}

func (w *shellWords) add(r rune) {
	w.word.WriteRune(r)
	w.inWord = true
}

func (w *shellWords) end() {
	if w.inWord {
		w.words = append(w.words, w.word.String())
		w.word.Reset()
		w.inWord = false
	}
}

// shlexSplit splits a command line the way a POSIX shell words it: spaces
// separate, quotes group, a backslash takes the next character as it is.
func shlexSplit(line string) ([]string, error) {
	var w shellWords
	quote := rune(0)
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		last := i+1 == len(runes)
		switch {
		case quote == '\'' && r == '\'', quote == '"' && r == '"':
			quote = 0
		case quote == '"' && r == '\\' && last:
			return nil, errNoEscaped
		case quote == '"' && r == '\\' && (runes[i+1] == '"' || runes[i+1] == '\\'):
			i++
			w.add(runes[i])
		case quote != 0:
			w.add(r)
		case strings.ContainsRune(" \t\r\n", r):
			w.end()
		case r == '\\' && last:
			return nil, errNoEscaped
		case r == '\\':
			i++
			w.add(runes[i])
		case r == '\'' || r == '"':
			quote, w.inWord = r, true
		default:
			w.add(r)
		}
	}
	if quote != 0 {
		return nil, errNoClosing
	}
	w.end()
	return w.words, nil
}

const (
	defaultPager  = "less"
	fallbackPager = "more"
)

// runPager shows text in a pager on the person's terminal.
func runPager(argv []string, text string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(text)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil // the pager ran; how it ended is the person's business
	}
	return err
}

// preview shows the whole SKILL.md (or README) in the user's pager, before
// anything is accepted.
func (a *app) preview(c candidate) {
	pager, err := shlexSplit(envOr("PAGER", defaultPager))
	if err == nil && len(pager) > 0 {
		err = a.runPager(pager, printable(c.Name+" · "+kindText(c)+"\n\n"+c.Doc))
	} else if err == nil {
		err = errors.New("PAGER names no program")
	}
	if err == nil {
		return
	}
	a.logf("pager: %v", err)
	if err := a.runPager([]string{fallbackPager}, printable(c.Doc)); err != nil {
		a.logf("pager: %v", err)
	}
}
