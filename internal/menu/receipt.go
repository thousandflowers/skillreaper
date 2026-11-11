package menu

import (
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/term"
)

const (
	receiptRows, receiptCols = 19, 40 // the warnings, the question and at least two rows of what it is about
	sectionRows, sectionCols = 13, 30
	bannerRows               = 16 // below this the next list's name is plain text
	receiptMinLines          = 3  // a receipt shows at least this many of its rows, or all of them
	// yesGuard is longer than the keyboard's repeat delay: a held y must not
	// answer a question it never saw.
	yesGuard = 550 * time.Millisecond
	// repeatGap: a y this close to the last key is a repeat or a paste, not an answer.
	repeatGap = 150 * time.Millisecond
	latinHint = "keys are read as Latin letters: switch keyboard layout"
)

const (
	unitTokens = "tokens"
	unitMoney  = "money"
)

// drawApplyLabel shows how many changes wait. hidden is the chosen rows the
// platform filter does not show, which apply all the same.
func (m *Menu) drawApplyLabel(h, wd, count, hidden int) {
	if count == 0 {
		return
	}
	label := " " + term.ApplyKeyName(m.s) + " apply " + itoa(count) + " change"
	if count != 1 {
		label += "s"
	}
	if hidden > 0 {
		label += " · " + itoa(hidden) + " not shown"
	}
	label += " "
	m.put(h-3, wd-runes(label)-4, label, m.hl())
}

// sectionEnd stands between two lists: what you just did, what comes next. Full
// screen, one key. It returns the list to go on with, or nil to stay.
func (m *Menu) sectionEnd(name string, items []*Row, nextName string, following []*Row, p *Plan, unitFmt func(int, float64) string, goingUp bool, onApply func() string) []*Row {
	advance := term.KeyDown
	if goingUp {
		advance = term.KeyUp
	}
	for {
		if m.tooSmall(sectionRows, sectionCols, "esc goes back") {
			if k := m.s.ReadKey(-1); k == term.KeyEsc || k == 'q' || k == term.KeyEOF {
				return nil
			}
			continue
		}
		var done []*Row
		for _, r := range items {
			if p.has(r) {
				done = append(done, r)
			}
		}
		tok, priced := m.weigh(done, func(r *Row) int { return saving(r, p.get(r)) })
		m.s.Erase()
		h, wd := m.s.Size()
		y := max(2, h/2-8)
		title := name + " · done"
		if goingUp {
			title = name
		}
		m.put(y, 3, title, dim)
		said := "nothing chosen out of " + itoa(len(items))
		if len(done) > 0 {
			said = itoa(len(done)) + " of " + itoa(len(items)) + " chosen"
			if tok > 0 {
				said += " · " + unitFmt(tok, priced)
			}
		}
		m.put(y+1, 3, said, asBold(m.tint(tintGreen)))
		m.grad(y+3, 3, wd-7, float64(len(done))/float64(max(1, len(items))), health)
		if len(following) > 0 {
			m.drawNext(y, h, wd, nextName, following, p, unitFmt, goingUp)
		} else {
			edge := "That was the last list."
			if goingUp {
				edge = "This is the first list."
			}
			m.put(y+6, 3, edge, bold)
			m.put(h-2, 3, "esc back, then "+term.ApplyKeyName(m.s)+" to apply", dim)
		}
		m.drawApplyLabel(h, wd, p.size(), 0)
		m.s.Refresh()
		k := m.s.ReadKey(-1)
		switch {
		case k == term.KeyResize || k == term.KeyMouse:
			continue
		case k == term.KeyEOF:
			return nil
		case onApply != nil && term.IsApply(m.s, k, p.size() > 0):
			onApply()
			continue
		case len(following) > 0 && (k == advance || k == term.KeyEnter):
			return following
		case k == term.KeyEsc || k == term.KeyUp || k == term.KeyDown || k == term.KeyLeft || k == 'q' || k == term.KeyEnter:
			return nil
		}
	}
}

func (m *Menu) drawNext(y, h, wd int, nextName string, following []*Row, p *Plan, unitFmt func(int, float64) string, goingUp bool) {
	var left []*Row
	for _, r := range following {
		if best(r) != "" && !p.has(r) {
			left = append(left, r)
		}
	}
	word, keys := "next", "↓ or enter continue"
	if goingUp {
		word, keys = "previous", "↑ or enter go back to it"
	}
	m.put(y+6, 3, word, dim)
	title := fitTitle(nextName, wd-8)
	summaryY := y + 8
	if h >= bannerRows {
		for row, text := range banner(title) {
			m.put(y+7+row, 3, text, asBold(m.tint(tintGreen)))
		}
		if title != nextName || !inFont(title) {
			m.put(y+10, 3, nextName, dim)
		}
		summaryY = y + 11
	} else {
		m.put(y+7, 3, nextName, asBold(m.tint(tintGreen)))
	}
	tok, priced := m.weigh(left, rowTokens)
	m.put(summaryY, 3, plural(len(following), "item")+" · "+itoa(len(left))+" left to choose · "+unitFmt(tok, priced), bold)
	m.put(h-2, 3, keys+" · esc stay here", dim)
}

// yesOrNo waits for an answer: arrows, clicks and any other key leave the screen
// as it is. A y counts only as a fresh key: not typed ahead, pasted, still held
// from the question before, or under a window too small to show the question.
func (m *Menu) yesOrNo(redraw func() bool, allowed bool) bool {
	asked := m.now()
	m.s.FlushInput()
	for {
		switch k := m.s.ReadKey(-1); k {
		case term.KeyResize:
			if redraw != nil {
				allowed = redraw()
			}
		case term.KeyNonLatin:
			h, _ := m.s.Size()
			m.put(h-1, 3, "y and n are read as Latin letters: switch keyboard layout", asBold(m.tint(tintAmber)))
			m.s.Refresh()
		case 'y':
			if allowed && m.now().Sub(asked) >= yesGuard && m.s.KeyGap() >= repeatGap {
				return true
			}
		case 'n', term.KeyEsc, term.KeyEOF:
			return false
		}
	}
}

// receiptPicks is every change the label counts, what cannot be undone first, so
// it survives a short window.
func (m *Menu) receiptPicks(p *Plan) []Pick {
	var picks []Pick
	for _, r := range m.rows {
		if p.has(r) {
			picks = append(picks, Pick{r, p.get(r)})
		}
	}
	rank := func(pk Pick) int {
		switch pk.Verb {
		case vDelete:
			return 0
		case vDisable:
			return 1
		}
		return 2
	}
	sort.SliceStable(picks, func(i, j int) bool {
		if rank(picks[i]) != rank(picks[j]) {
			return rank(picks[i]) < rank(picks[j])
		}
		return picks[i].Row.Tokens > picks[j].Row.Tokens
	})
	return picks
}

var receiptTag = map[string]string{vDelete: "⚠ delete", vUnmute: "undo", vUnlock: "undo", vRestore: "undo"}

func (m *Menu) receiptLine(pk Pick, pw int) string {
	if pk.Verb == vDisable {
		return pad("disable", 9) + cutMid(pk.Row.Source[len(sourcePlugin):], pw-9)
	}
	kept := saving(pk.Row, pk.Verb)
	worth := ""
	switch {
	case pk.Row.WeightUnknown:
		worth = "?"
	case kept > 0:
		worth = m.tokMoney(kept, float64(kept)*m.reach(pk.Row))
	}
	amountW := max(cells(worth), min(20, pw/3))
	nameW := pw - 9 - amountW
	tag := pk.Verb
	if t, ok := receiptTag[pk.Verb]; ok {
		tag = t
	}
	return pad(tag, 9) + pad(cutMid(m.displayName(pk.Row), nameW), nameW) + padLeft(worth, amountW)
}

// doneSaid is what became of the picks, by what happened, in a fixed order.
func doneSaid(picks []Pick) string {
	counts := map[string]int{}
	var order []string
	verbs := map[string]bool{}
	for _, pk := range picks {
		verbs[pk.Verb] = true
	}
	for _, w := range doneWords {
		if verbs[w[0]] && counts[w[1]] == 0 {
			order = append(order, w[1])
			counts[w[1]] = -1
		}
	}
	for word := range counts {
		counts[word] = 0
	}
	for _, pk := range picks {
		for _, w := range doneWords {
			if w[0] == pk.Verb {
				counts[w[1]]++
			}
		}
	}
	var out []string
	for _, word := range order {
		out = append(out, itoa(counts[word])+" "+word)
	}
	return strings.Join(out, " · ")
}

// receipt shows what is about to change, asks, and only on a y hands the picks
// to the Applier. It returns what to say on the list afterwards.
func (m *Menu) receipt(p *Plan, unit string, onApplied func()) string {
	was := m.s.SetMouse(false)
	defer m.s.SetMouse(was)
	picks := m.receiptPicks(p)
	if len(picks) == 0 {
		return "nothing to apply"
	}
	rc := newReceiptView(m, picks, unit)
	if rc.gone > 0 {
		m.s.Beep()
	}
	if !m.yesOrNo(func() bool { return rc.draw("Do it?  [y/n]") }, rc.draw("Do it?  [y/n]")) {
		return "cancelled: your choices are still here"
	}
	rc.draw("working…")
	outcomes := m.apply.Apply(picks)
	m.settle(outcomes)
	onApplied()
	var done []Pick
	var failed []Outcome
	for _, o := range outcomes {
		if o.Err != nil {
			failed = append(failed, o)
		} else {
			done = append(done, o.Pick)
		}
	}
	said := ""
	if len(done) > 0 {
		said = "✓ " + doneSaid(done)
		for _, pk := range done {
			if undoText[pk.Verb] != "" {
				said += ": back in the list at the next launch"
				break
			}
		}
	}
	if len(failed) > 0 {
		first := failed[0]
		warn := "⚠ " + itoa(len(failed)) + " not done: " + first.Pick.Row.Name + ": " + first.Err.Error()
		if said != "" {
			warn += " · " + said
		}
		return warn
	}
	return said
}

type receiptView struct {
	m        *Menu
	picks    []Pick
	lines    []Pick
	unit     string
	warnings []string
	using    string
	tok      int
	priced   float64
	back     int
	gone     int
}

func newReceiptView(m *Menu, picks []Pick, unit string) *receiptView {
	v := &receiptView{m: m, picks: picks, unit: unit, using: usingWarning(picks)}
	seen := map[string]bool{}
	for _, pk := range picks {
		t := saving(pk.Row, pk.Verb)
		v.tok += t
		v.priced += float64(t) * m.reach(pk.Row)
		switch pk.Verb {
		case vUnmute, vRestore:
			v.back++
		case vDelete:
			v.gone++
		}
		// One line for a whole plugin turned off.
		if pk.Verb == vDisable {
			if seen[pk.Row.Source] {
				continue
			}
			seen[pk.Row.Source] = true
		}
		v.lines = append(v.lines, pk)
	}
	if linked := sharedFolders(picks); len(linked) > 0 {
		v.warnings = append(v.warnings, "⚠ also muted for other tools using "+strings.Join(linked, ", "))
	}
	if taken := removedForOthers(picks); len(taken) > 0 {
		v.warnings = append(v.warnings, "⚠ also gone for other tools using "+strings.Join(taken, ", "))
	}
	if w := updateWarning(picks); w != "" {
		v.warnings = append(v.warnings, w)
	}
	return v
}

// draw returns false when the window cannot show the question: then y must not
// answer it.
func (v *receiptView) draw(question string) bool {
	m := v.m
	h, wd := m.s.Size()
	extra := len(v.warnings)
	if v.gone > 0 {
		extra++
	}
	// A y must never answer a list it cannot see: with many warnings a short
	// window would have room for none of the rows.
	room := h - 14 - max(2, extra)
	need := receiptRows + max(0, min(len(v.lines), receiptMinLines)-room)
	if m.tooSmall(need, receiptCols, "too small to confirm: esc cancels") {
		return false
	}
	m.s.Erase()
	pw := min(60, wd-6)
	x := (wd - pw) / 2
	title := "SKILLREAPER"
	m.put(1, x+(pw-len(title))/2, title, bold)
	m.put(2, x, strings.Repeat("┄", pw), dim)
	shown := v.lines
	if len(shown) > room {
		shown = shown[:max(0, room-1)]
	}
	for n, pk := range shown {
		st := dim
		switch pk.Verb {
		case vDelete:
			st = asBold(m.red())
		case vDisable:
			st = asBold(m.tint(tintAmber))
		}
		m.put(3+n, x, m.receiptLine(pk, pw), st)
	}
	if len(shown) < len(v.lines) {
		m.put(3+len(shown), x, "… and "+itoa(len(v.lines)-len(shown))+" more", dim)
	}
	y := h - 11
	m.put(y, x, strings.Repeat("┄", pw), dim)
	shift := 1
	if v.gone > 0 {
		it := "they"
		if v.gone == 1 {
			it = "it"
		}
		m.put(y-1, x, pad(" ⚠ "+itoa(v.gone)+" deleted for good: "+it+" cannot be restored ", pw), asBold(m.rampColour(health, 0, true)))
		shift = 2
	}
	for n, line := range v.warnings {
		m.put(y-shift-n, x, cut(line, pw), asBold(m.tint(tintAmber)))
	}
	summary := plural(len(v.picks), "item")
	if v.tok > 0 {
		summary += " · ~" + m.tokens(v.tok) + " tokens lighter every session"
	}
	switch {
	case v.back == 1:
		summary += " · 1 comes back heavier"
	case v.back > 1:
		summary += " · " + itoa(v.back) + " come back heavier"
	}
	m.put(y+1, x, cut(summary, pw), bold)
	if v.tok > 0 {
		above, beside, figure := "", "tokens saved every session", m.tokens(v.tok)
		if v.unit == unitMoney {
			above, beside = m.bigMoneyWords("saved every month")
			figure = m.money(v.priced)
		}
		m.put(y+2, x, above, dim)
		end := m.big(y+3, x, figure, 0) // green: what you get back, not what you waste
		m.put(y+4, end+2, beside, asBold(m.tint(tintGreen)))
	}
	if v.using != "" {
		m.put(y+6, x, cut(v.using, pw), asBold(m.tint(tintAmber)))
	}
	m.put(y+7, x, question, bold)
	m.s.Refresh()
	return true
}
