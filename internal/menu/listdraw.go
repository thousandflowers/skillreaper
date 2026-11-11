package menu

import (
	"math"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/term"
)

var listKeys = []string{"enter open", "space choose", "a auto", "p prune", "m mute", "l lock", "d delete", "n nothing", "u undo", "q quit"}

// usesField is how often a row fired. The word goes before the figure does.
func (m *Menu) usesField(n int) string {
	words := m.shortCount(n) + " use"
	if n != 1 {
		words += "s"
	}
	if runes(words) > usesWidth {
		words = m.shortCount(n)
	}
	return pad(words, usesWidth)
}

// rowTag is the small word on a row: what is about to happen if that cannot be
// undone, else what is true of it.
func rowTag(r *Row, choice, refused string) string {
	switch {
	case refused != "":
		return "can't " + refused
	case choice == vDelete:
		return "⚠ delete"
	case choice == vUnmute || choice == vUnlock || choice == vRestore:
		return "undo"
	case readOnly(r):
		return suggestRO
	case r.MuteLost:
		return "mute lost"
	case r.PruneLost:
		return "prune lost"
	case r.Reason == reasonMuted:
		return "muted"
	case alreadyLocked(r):
		return "locked"
	}
	return ""
}

// or is the cursor's highlight when there is one, else the row's own look.
func or(base term.Style, has bool, own term.Style) term.Style {
	if has {
		return base
	}
	return own
}

// draw paints the whole list and reports whether something is still moving.
func (l *list) draw() bool {
	m := l.m
	target := float64(l.target)
	l.shown = easeTo(l.shown, target)
	// The headline falls only by what it counted, the rows reap calls wasted, and
	// at its own pace: a mute that was never in it must not make it jump.
	l.shownDead = easeTo(l.shownDead, float64(l.dead))
	left := math.Max(0, float64(l.total)-l.shownDead)
	eased, deadShare := 0.0, 0.0
	if target != 0 {
		eased = l.shown / target // the money saved rises with the number
	}
	if l.dead != 0 {
		deadShare = l.shownDead / float64(l.dead)
	}
	m.s.Erase()
	h, wd := m.s.Size()
	frac := left / math.Max(1, float64(l.total))
	l.header(wd, left, frac, eased, deadShare)
	if len(m.warnings) > 0 {
		note := cut("⚠ "+m.warnings[0], wd-18)
		if more := len(m.warnings) - 1; more > 0 {
			note += " · +" + itoa(more) + " more"
		}
		m.put(4, 3, note, m.tint(tintAmber))
	}
	m.grad(5, 3, wd-7, frac, ramp)
	n := wd - 8 - amountColumn - min(wideSlack, max(0, wd-88))
	amountW := wd - 5 - (3 + n)
	title := "NAME · " + modeNames[l.mode]
	if l.plat > 0 {
		title += "  (" + l.platformLabel() + ")"
	}
	m.put(6, 3, pad(title, n-18), dim)
	label := "TOKENS"
	if l.unit == unitMoney {
		label = m.moneyColumn()
	}
	m.put(6, wd-5-len(label), label, dim)

	l.view = h - 12
	if l.heldTop >= 0 && !(l.heldTop <= l.i && l.i < l.heldTop+l.view) {
		l.heldTop = -1
	}
	l.top = window(l.i, l.view, len(l.flat))
	if l.heldTop >= 0 {
		l.top = max(0, min(l.heldTop, len(l.flat)-l.view))
	}
	l.rowmap = map[int]int{}
	l.animating = false
	for row := 0; row < l.view && l.top+row < len(l.flat); row++ {
		idx := l.top + row
		l.rowmap[listY+row] = idx
		l.drawLine(listY+row, idx, n, amountW)
	}
	l.footer(h, wd)
	m.s.Refresh()
	return l.shown != target || l.shownDead != float64(l.dead) || l.animating
}

// easeTo moves a shown figure part of the way to where it is going, and lands
// it once it is close.
func easeTo(shown, target float64) float64 {
	step := shown + (target-shown)*ease
	if math.Abs(target-step) < snap || step == shown {
		return target
	}
	return step
}

// header is the big number and what it means, in money or in tokens.
func (l *list) header(wd int, left, frac, eased, deadShare float64) {
	m := l.m
	green := asBold(m.tint(tintGreen))
	switch {
	case m.rep.Sessions == 0:
		m.put(1, 3, "no sessions read: nothing is measured yet", bold)
		m.put(2, 3, "the verdicts below have no evidence behind them", dim)
	case l.unit == unitMoney:
		savedPriced := l.targetPriced * eased
		value := math.Max(0, l.totalPriced-l.deadPriced*deadShare) * m.price * m.cur.Rate
		above, words := m.bigMoneyWords("wasted a month")
		figure := m.moneyFigure(value)
		if bigWidth(figure) > wd-5-len(words) {
			figure = m.cur.Sym + m.short(value)
		}
		m.put(0, 3, above, dim)
		end := m.big(1, 3, figure, frac)
		m.put(1, end+2, words, bold)
		m.put(2, end+2, "~"+m.tokens(int(left))+" tokens re-sent every session", dim)
		if l.shown > 0 {
			m.put(3, end+2, "you saved "+m.worth(m.money(savedPriced))+" a month", green)
		}
	default:
		words := "tokens wasted every session"
		figure := m.tokens(int(left))
		if bigWidth(figure) > wd-5-len(words) {
			figure = m.short(left)
		}
		end := m.big(1, 3, figure, frac)
		m.put(1, end+2, words, bold)
		if l.totalMonth > 0 {
			month := "~" + m.short(math.Max(0, l.totalMonth-l.deadMonth*deadShare)) + " in a month of sessions"
			if m.subscriber {
				month += " · what eats your plan"
			}
			m.put(2, end+2, month, dim)
		}
		if l.shown > 0 {
			m.put(3, end+2, "you saved ~"+m.tokens(int(l.shown))+" tokens per session", green)
		}
	}
	moneyTab, tokenTab := " "+m.cur.Sym+" money ", " tokens "
	tokenX := wd - 4 - len(tokenTab)
	m.put(0, tokenX-1-runes(moneyTab), moneyTab, or(m.hl(), l.unit == unitMoney, dim))
	m.put(0, tokenX, tokenTab, or(m.hl(), l.unit == unitTokens, dim))
}

func (l *list) drawLine(y, idx, n, amountW int) {
	m := l.m
	f := l.flat[idx]
	cur := idx == l.i
	mark := " "
	if cur {
		mark = ">"
	}
	m.put(y, 1, mark, bold)
	lit := cur || (l.lo <= idx && idx <= l.hi) || l.painted[idx]
	if f.isGroup {
		l.drawGroup(y, f, n, amountW, lit)
		return
	}
	l.drawItem(y, f.row, n, amountW, lit)
}

func (l *list) drawGroup(y int, f line, n, amountW int, lit bool) {
	m := l.m
	hl, green := m.hl(), m.tint(tintGreen)
	st := l.statsOf(f.gn)
	on := len(st.marked) > 0
	arrow := "▸"
	if l.opened[f.name] {
		arrow = "▾"
	}
	text := st.box + " " + arrow + " " + cutMid(f.name, n-24)
	if st.muted > 0 && st.muted != len(f.rows) {
		text += " · " + itoa(st.muted) + " muted"
	}
	if st.deletes > 0 {
		text += " · ⚠ " + itoa(st.deletes) + " delete"
	}
	field := pad(text, n-18)
	boxLook := bold
	switch {
	case st.box == "[d]":
		boxLook = asBold(m.red())
	case on:
		boxLook = green
	}
	m.put(y, 3, field[:3], or(hl, lit, boxLook))
	nameLook := bold
	switch {
	case st.deletes > 0:
		nameLook = asBold(m.red())
	case st.faded:
		nameLook = dim
	}
	name := field[3:]
	if st.full {
		name = strike(name)
	}
	m.put(y, 6, name, or(hl, lit, nameLook))
	phrase := m.groupPhrase(f.rows, st)
	if l.peek {
		phrase = peekPhrase(f.rows)
	}
	phraseW := max(18, runes(phrase))
	m.put(y, 3+n-phraseW, pad(phrase, phraseW), or(hl, lit, or(green, on, dim)))
	tok, priced := st.total, st.totalPriced
	if on {
		tok, priced = st.saved, st.savedPriced
	}
	amount := ""
	if tok > 0 {
		amount = l.unitFmt(tok, priced)
	}
	m.put(y, 3+n, padLeft(amount, amountW), or(hl, lit, or(green, on, bold)))
}

func (l *list) drawItem(y int, r *Row, n, amountW int, lit bool) {
	m, p := l.m, l.p
	hl, green, amber := m.hl(), m.tint(tintGreen), m.tint(tintAmber)
	verb := p.get(r)
	gone := lighter[verb]
	box := stateBox(r, p)
	boxLook := plain
	switch {
	case box == "[d]":
		boxLook = asBold(m.red())
	case verb != "":
		boxLook = green
	}
	m.put(y, 3, "  "+box, or(hl, lit, boxLook))
	name := []rune(" " + pad(cutMid(m.displayName(r), n-26), n-24))
	m.put(y, 3+n-18, m.usesField(r.Uses), or(hl, lit, or(plain, r.Uses > 0, dim)))
	shown := string(name)
	if struck[verb] {
		at, started := p.sweep[r]
		if !started {
			at = len(name)
		}
		at = min(len(name), at+strikeStep)
		p.sweep[r] = at
		l.animating = l.animating || at < len(name)
		shown = strike(string(name[:at])) + string(name[at:])
	}
	m.put(y, 8, shown, or(hl, lit, or(dim, gone || best(r) == "", plain)))
	tag := rowTag(r, verb, p.refused[r])
	tagLook := green
	switch {
	case l.peek:
		tag, tagLook = suggestionWord(r), amber
	case verb == vDelete:
		tagLook = asBold(m.red())
	case verb == vMute || amberTags[tag]:
		tagLook = amber
	case tag == suggestRO:
		tagLook = dim
	}
	weight := r.Tokens
	if gone {
		weight = saving(r, verb)
	}
	cost := ""
	switch {
	case r.WeightUnknown:
		cost = "?"
	case r.Tokens > 0:
		cost = l.unitFmt(weight, float64(weight)*m.reach(r))
	}
	m.put(y, 3+n, padLeft(cost, amountW), or(hl, lit, or(green, gone, dim)))
	// After the amount: a tag longer than its column borrows the amount's empty left edge.
	m.put(y, 3+n-10, pad(tag, max(10, cells(tag))), or(hl, lit, tagLook))
}

// footer is the hint for the cursor, what is chosen so far, and the keys.
func (l *list) footer(h, wd int) {
	m := l.m
	cur := l.flat[l.i]
	var hint string
	switch {
	case l.anchor >= 0:
		items := l.chosen(rowItems)
		how := "shift+↑↓"
		if l.marking {
			how = "arrows"
		}
		tok, priced := m.weigh(items, rowTokens)
		hint = "SELECTING " + plural(l.hi-l.lo+1, "row") + " · " + plural(len(items), "item") + " · " + l.unitFmt(tok, priced) +
			" · " + how + " to extend · space prunes them · esc cancels"
	case l.peek:
		hint = "reap suggests, while you hold s"
	case cur.isGroup:
		hint = m.cursorHint(cur, l.statsOf(cur.gn), l.p)
	default:
		hint = m.cursorHint(cur, stats{}, l.p)
	}
	m.put(h-4, 3, cut(hint, wd-6), or(asBold(m.tint(tintAmber)), l.marking || l.peek, bold))
	status := l.msg
	if status == "" || isEcho(status) {
		status = planSummary(l.p)
	}
	m.put(h-3, 3, cut(status, wd-28), or(asBold(m.red()), strings.HasPrefix(status, "⚠"), m.tint(tintGreen)))
	hidden := 0
	for r := range l.p.verbs {
		if !l.visible(r) {
			hidden++
		}
	}
	m.drawApplyLabel(h, wd, l.p.size(), hidden)
	keys := append(append([]string(nil), listKeys...), "tab "+m.cur.Sym+"/tok", "g group by", "f platform", "shift+↑↓ many", "o plugin off", "hold s reap", "v mark")
	m.legend(h, keys)
}
