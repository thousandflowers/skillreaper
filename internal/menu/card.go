package menu

import (
	"strings"

	"github.com/thousandflowers/skillreaper/internal/term"
)

const (
	cardRows, cardCols = 11, 58 // the action row and the keys must fit
	choiceNothing      = "nothing"
	heavyTokens        = 300 // a row this heavy fills the weight bar
)

var (
	explain = map[string]string{
		choiceNothing: "leave it as it is", vLock: "locked: reap never suggests touching it",
		vMute:    "keeps it, strips its description, reap unmute brings it back",
		vPrune:   "moved to quarantine, reap restore brings it back",
		vDelete:  "⚠ gone for good, it cannot be restored",
		vDisable: "goes with its whole plugin: claude plugin disable",
	}
	choiceKey = map[string]rune{choiceNothing: 'n', vLock: 'l', vMute: 'm', vPrune: 'p', vDelete: 'd'}
	undoText  = map[string]string{vUnmute: "brings its description back", vUnlock: "reap may suggest touching it again", vRestore: "brings it back from quarantine"}
)

type option struct {
	label   string
	allowed bool
}

// cardOptions is the card's five, each with whether reap could carry it out on
// this row.
func cardOptions(r *Row) []option {
	return []option{
		{choiceNothing, true},
		{vLock, can(r, vLock) || existing(r) == vLock},
		{vMute, can(r, vMute) || existing(r) == vMute},
		{vPrune, can(r, vPrune)},
		{vDelete, can(r, vDelete)},
	}
}

// cardHooks is what the card asks of the list it was opened from.
type cardHooks struct {
	act      func(rows []*Row, ch rune)
	onEnd    func(cur []*Row) []*Row // past the last row: the next list, or nil
	onStart  func(cur []*Row) []*Row // before the first row: the previous list, or nil
	onApply  func() string
	pending  func() int
	onAuto   func(rows []*Row) string
	onQuit   func() bool
	onPlugin func(r *Row) string
}

func (m *Menu) cardTitle(r *Row, fit string, weight float64, fate string) {
	colour := m.rampColour(ramp, weight, false)
	if !inFont(fit) {
		m.put(2, 3, r.Name, asBold(colour)) // the big font has no letters for this name
		return
	}
	st := bold
	if !m.s.Light() {
		st = asBold(colour)
	}
	if fate == vMute {
		st.Bold, st.Dim = false, true
	}
	for row, text := range banner(fit) {
		// Colour is the weight. The fate is the shape: struck through, or faded.
		if struck[fate] && row == 1 {
			text = strings.Repeat("━", runes(strings.TrimRight(text, " ")))
		}
		m.put(1+row, 3, text, st)
	}
}

func cardExplain(r *Row, effective string) string {
	note := explain[effective]
	switch {
	case effective == vMute && isPlugin(r):
		return note + " · a plugin update can bring the description back"
	case (effective == vPrune || effective == vDelete) && isPlugin(r):
		return note + " · a plugin update can bring it back"
	}
	return note
}

func (m *Menu) cardUsage(r *Row) string {
	seen := lastUsed(r.LastUsed, m.now())
	switch {
	case seen == "never" && !r.HistoricalLast.IsZero():
		seen = lastUsed(r.HistoricalLast, m.now()) + ", before this window"
	case seen == "never" && r.Reason == "rare":
		seen = "not in this window"
	}
	span := "in this window"
	if days := m.rep.WindowDays; days > 0 {
		span = "in " + itoa(days) + " days"
	}
	return plural(r.Uses, "use") + " " + span + " · last used " + seen
}

func labelIndex(options []option, label string) int {
	for n, o := range options {
		if o.label == label {
			return n
		}
	}
	return 0
}

// card is one row on a page of its own, the same page as the list: big name on
// top, bar under it, hint and keys at the bottom. Every row is fixed, so
// scrolling fast changes the words and nothing moves. It returns the row it ended
// on, or nil to leave the menu.
func (m *Menu) card(r *Row, p *Plan, siblings []*Row, hk cardHooks) *Row {
	if len(siblings) == 0 {
		siblings = []*Row{r}
	}
	at := 0
	for n, x := range siblings {
		if x == r {
			at = n
		}
	}
	notice := ""
	var focusOn *Row
	focus := 0
	for {
		r = siblings[at]
		if m.tooSmall(cardRows, cardCols, "esc goes back") {
			if k := m.s.ReadKey(-1); k == term.KeyEsc || k == 'q' || k == term.KeyEOF {
				return r
			}
			continue
		}
		chosen := p.get(r)
		state := existing(r)
		effective := firstOf(chosen, firstOf(state, choiceNothing))
		if undoText[chosen] != "" {
			effective = choiceNothing
		}
		options := cardOptions(r)
		if focusOn != r {
			focusOn, focus = r, labelIndex(options, effective)
		}
		if ended := p.takeEnded(); ended != "" {
			notice = "plugin off cancelled: it takes the whole plugin"
		}
		count := 0
		if hk.pending != nil {
			count = hk.pending()
		}
		m.drawCard(r, at, len(siblings), chosen, effective, options, focus, notice, count)
		k := m.s.ReadKey(-1)
		if k == term.KeyMouse || k == term.KeyResize {
			continue // the card has no use for the mouse: what it says stays
		}
		notice = ""
		switch {
		case k == term.KeyEOF:
			return nil
		case k == term.KeyNonLatin:
			notice = latinHint
		case hk.onApply != nil && term.IsApply(m.s, k, count > 0):
			notice = hk.onApply()
		case k == 'q' && hk.onQuit != nil:
			if hk.onQuit() {
				return nil
			}
		case k == term.KeyEsc || k == 'q' || k == term.KeyShiftEnter || k == term.KeyEnter:
			return r
		case k == 'o' && hk.onPlugin != nil:
			notice = hk.onPlugin(r)
			focusOn = nil
		case k == 'a' && hk.onAuto != nil:
			notice = hk.onAuto([]*Row{r})
			focusOn = nil
		case k == term.KeyLeft || k == term.KeyRight:
			step := 1
			if k == term.KeyLeft {
				step = -1
			}
			moved := min(len(options)-1, max(0, focus+step)) // no wrap: left of nothing is not delete
			if moved == focus {
				continue
			}
			focus = moved
			m.choose(r, p, options[focus], state, hk)
		case k == term.KeyUp:
			if at == 0 && hk.onStart != nil {
				if earlier := hk.onStart(siblings); len(earlier) > 0 {
					siblings, at = earlier, len(earlier)-1
				}
				continue
			}
			at = max(0, at-1)
		case k == term.KeyDown:
			if at == len(siblings)-1 && hk.onEnd != nil {
				if following := hk.onEnd(siblings); len(following) > 0 {
					siblings, at = following, 0
				}
				continue
			}
			at = min(len(siblings)-1, at+1)
		case k == ' ':
			// As in the list: space means what reap can do to it at once, even
			// over another choice, and clears only that.
			if verb := spaceVerb(r); verb != 0 {
				if chosen == map[rune]string{'p': vPrune, 'm': vMute}[verb] {
					verb = 'n'
				}
				hk.act([]*Row{r}, verb)
			}
			focusOn = nil
		case k == 'n':
			hk.act([]*Row{r}, 'n')
			focusOn = nil
		case k == 'u':
			hk.act([]*Row{r}, 'u')
			focusOn = nil
		case k == 'p' || k == 'm' || k == 'l' || k == 'd':
			if refused := refusedOption(options, rune(k)); refused >= 0 {
				focus = refused // stand on the grey option: it says why
				continue
			}
			hk.act([]*Row{r}, rune(k))
			focusOn = nil
			if k == 'd' {
				m.s.Beep()
			}
		}
	}
}

func spaceVerb(r *Row) rune {
	switch {
	case can(r, vPrune):
		return 'p'
	case can(r, vMute):
		return 'm'
	}
	return 0
}

func refusedOption(options []option, ch rune) int {
	for n, o := range options {
		if !o.allowed && choiceKey[o.label] == ch {
			return n
		}
	}
	return -1
}

// choose is the card's arrows landing on an option. A grey one can be stood on
// and read; nothing changes.
func (m *Menu) choose(r *Row, p *Plan, pick option, state string, hk cardHooks) {
	if !pick.allowed || m.applied[r] != "" {
		return // a card can still be showing a row that just went
	}
	switch {
	case state != "" && pick.label == choiceNothing:
		p.set(r, existingUndo[state])
	case pick.label == state:
		p.pop(r)
	default:
		hk.act([]*Row{r}, choiceKey[pick.label])
	}
	if pick.label == vDelete {
		m.s.Beep()
	}
}

func (m *Menu) drawCard(r *Row, at, of int, chosen, effective string, options []option, focus int, notice string, count int) {
	m.s.Erase()
	h, wd := m.s.Size()
	short := r.Name
	if cutAt := strings.LastIndex(r.Name, ":"); cutAt >= 0 && cutAt < len(r.Name)-1 {
		short = r.Name[cutAt+1:]
	}
	weight := min(1, float64(r.Tokens)/heavyTokens)
	fit := fitTitle(short, wd-8)
	m.cardTitle(r, fit, weight, effective)
	place := itoa(at+1) + " of " + itoa(of)
	m.put(0, wd-len(place)-1, place, dim)
	name := short
	if fit != short {
		name = r.Name
	}
	source, _, _ := strings.Cut(r.Source, "@")
	m.put(4, 3, cut(name+" · "+r.cat()+" · "+source+" · "+firstOf(r.Platform, unknownTool), wd-6), dim)
	m.drawOptions(options, focus, effective)
	switch {
	case !options[focus].allowed:
		m.put(6, 3, options[focus].label+" is not available: "+whyNot(r, options[focus].label), asBold(m.tint(tintAmber)))
	case undoText[chosen] != "":
		m.put(6, 3, undoText[chosen], dim)
	case effective == vDelete:
		m.put(6, 3, cardExplain(r, effective), asBold(m.red()))
	default:
		m.put(6, 3, cardExplain(r, effective), dim)
	}
	// Rows above the path: when the window is short they give way, the action row
	// and the keys never do.
	body := func(y, x int, s string, st term.Style) {
		if y < h-5 {
			m.put(y, x, s, st)
		}
	}
	weightText := "weight unknown"
	if !r.WeightUnknown {
		weightText = "~" + m.tokens(r.Tokens) + " tokens every session"
		if m.price > 0 {
			body(7, max(34, 5+runes(weightText)), m.worth(m.money(float64(r.Tokens)*m.reach(r)))+" a month", dim)
		}
	}
	body(7, 3, weightText, bold)
	if 8 < h-5 {
		m.grad(8, 3, wd-7, max(weight, 0.01), ramp)
	}
	body(10, 3, suggestionLine(r), bold)
	body(11, 3, whyLine(r), dim)
	body(13, 3, m.cardUsage(r), bold)
	body(14, 3, plural(r.ErrorCount, "error"), dim)
	if room := max(0, h-23); room > 0 {
		// Only what the visible lines can hold is wrapped: a huge description
		// must not slow every key.
		desc := cut(strings.TrimSpace(r.Description), wd*(room+1))
		if desc == "" {
			desc = "no description"
		}
		lines := wrap(desc, wd-8)
		for n, line := range lines[:min(room, len(lines))] {
			m.put(17+n, 3, line, plain)
		}
	}
	m.put(h-5, 3, cut(r.Path, wd-8), dim)
	if notice != "" {
		m.put(h-3, 3, cut(notice, wd-6), asBold(m.tint(tintAmber)))
	}
	keys := []string{"enter or esc back", "←→ choose", "↑↓ next skill", "a auto", "n clear", "u undo", "l lock", "m mute", "p prune", "d delete"}
	if isPlugin(r) {
		keys = append(keys, "o plugin off")
	}
	m.legend(h, append(keys, "q quit"))
	m.drawApplyLabel(h, wd, count, 0)
	m.s.Refresh()
}

func (m *Menu) drawOptions(options []option, focus int, effective string) {
	x := 3
	for n, o := range options {
		on := effective == o.label
		grey := dim
		if n == focus {
			grey = term.Style{Reverse: true, Dim: true}
		}
		if o.label == vDelete {
			st := asBold(m.red())
			switch {
			case !o.allowed:
				st = grey
			case on:
				st = asBold(m.rampColour(health, 0, true))
			}
			m.put(5, x, "  ⚠ delete  ", st)
			x += 2
		} else {
			st := plain
			switch {
			case on:
				st = m.hl()
			case !o.allowed:
				st = grey
			}
			m.put(5, x, "  "+o.label+"  ", st)
		}
		x += len(o.label) + 5
	}
	if effective == vDisable {
		m.put(5, x, "  plugin off  ", m.hl())
	}
}
