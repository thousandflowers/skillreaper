package menu

import (
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

const (
	listRows, listCols = 14, 58 // two rows of list and an opened group's name column
	amountColumn       = 14     // the amount's room on a narrow window; a wide one keeps more air
	wideSlack          = 12
	ease               = 0.3 // the share of the way the big number moves each frame
	snap               = 2.0 // this close, it lands
	wheelRows          = 3
	listY              = 7 // first row of the list
	usesWidth          = 8
	frame              = 25 * time.Millisecond
	peekWait           = 550 * time.Millisecond // longer than the keyboard's repeat delay, or a held key flickers
	strikeStep         = 7
)

// amberTags are the row tags that say something is not as it looks.
var amberTags = map[string]bool{"can't mute": true, "can't prune": true, "can't delete": true, "muted": true, "locked": true,
	"mute lost": true, "prune lost": true, "undo": true, "pruned": true}

type repeat struct {
	key   term.Key
	items []*Row
}

// list is the main screen and everything it remembers between keys.
type list struct {
	m    *Menu
	p    *Plan
	mode int
	i    int
	plat int // 0 is every platform
	// anchor is where a selection started, -1 when there is none.
	anchor    int
	marking   bool
	opened    map[string]bool // by group name: groups renumber when one empties
	msg       string
	peek      bool
	shown     float64
	shownDead float64 // the part of shown the headline counts
	unit      string
	paint     int // -1 not painting, 0 clearing, 1 setting
	// rowmap is the list index under each screen row, for the mouse.
	rowmap      map[int]int
	lastPainted int
	heldTop     int
	repeat      *repeat
	painted     map[int]bool
	beforeOff   map[string]map[*Row]string
	focus       *Row
	left        bool // the card was asked to leave the menu

	gs    []group
	flat  []line
	stats map[int]stats
	built struct {
		mode, plat, applied, settled, version int
		ok                                    bool
	}
	total, target             int
	totalPriced, totalMonth   float64
	targetPriced, targetMonth float64
	// dead is the part of the target that is in the headline: what is chosen
	// among the rows the big number counts. A mute of a skill you do use saves
	// tokens too, but was never part of "wasted".
	dead                  int
	deadPriced, deadMonth float64
	top, view, lo, hi     int
	animating             bool
}

func newList(m *Menu) *list {
	l := &list{m: m, p: newPlan(), anchor: -1, paint: -1, heldTop: -1, lastPainted: -1,
		opened: map[string]bool{}, painted: map[int]bool{}, beforeOff: map[string]map[*Row]string{}, unit: unitMoney}
	if m.subscriber || m.price == 0 {
		l.unit = unitTokens
	}
	return l
}

func (l *list) visible(r *Row) bool {
	return l.plat == 0 || r.Platform == l.m.platforms[l.plat-1]
}

func (l *list) platformLabel() string {
	if l.plat == 0 {
		return "all"
	}
	switch p := l.m.platforms[l.plat-1]; p {
	case "":
		return unknownTool
	case "all":
		return `"all"`
	default:
		return p
	}
}

func (l *list) unitFmt(tok int, priced float64) string {
	if l.unit != unitMoney {
		return "~" + l.m.tokens(tok) + " tok"
	}
	text := l.m.money(priced) // each row rounds on its own: they add up only about
	if strings.HasPrefix(text, "<") {
		return text
	}
	return "~" + text
}

func (l *list) forgetPaint() {
	l.paint, l.lastPainted = -1, -1
	l.painted = map[int]bool{}
}

func (l *list) endSelection() { l.anchor, l.marking = -1, false }

// rebuild lays the list out again when something it depends on changed.
func (l *list) rebuild() {
	m := l.m
	b := &l.built
	if !b.ok || b.mode != l.mode || b.plat != l.plat || b.applied != len(m.applied) || b.settled != m.settled {
		l.gs = l.gs[:0]
		for _, g := range m.grouped(l.mode) {
			var rows []*Row
			for _, r := range g.rows {
				if l.visible(r) && m.applied[r] == "" {
					rows = append(rows, r)
				}
			}
			if len(rows) == 0 {
				continue
			}
			sort.SliceStable(rows, func(i, j int) bool {
				if (best(rows[i]) != "") != (best(rows[j]) != "") {
					return best(rows[i]) != ""
				}
				return rows[i].Tokens > rows[j].Tokens
			})
			l.gs = append(l.gs, group{g.name, rows})
		}
		waste := m.wasted(l.visible)
		l.total, l.totalPriced = m.weigh(waste, rowTokens)
		l.totalMonth = 0
		for _, r := range waste {
			l.totalMonth += float64(r.Tokens * m.sessionsOf(r))
		}
		b.version = -1
	}
	l.flat = l.flat[:0]
	for gn, g := range l.gs {
		if l.mode < 2 {
			l.flat = append(l.flat, line{isGroup: true, gn: gn, name: g.name, rows: g.rows})
		}
		if l.mode == 2 || l.opened[g.name] {
			for _, r := range g.rows {
				l.flat = append(l.flat, line{gn: gn, name: g.name, row: r, rows: g.rows})
			}
		}
	}
	if b.version != l.p.version {
		l.target, l.targetPriced, l.targetMonth = 0, 0, 0
		l.dead, l.deadPriced, l.deadMonth = 0, 0, 0
		count := func(r *Row, verb string) {
			if !l.visible(r) {
				return
			}
			t := saving(r, verb)
			l.target += t
			l.targetPriced += float64(t) * m.reach(r)
			l.targetMonth += float64(t * m.sessionsOf(r))
			if r.Verdict == report.VerdictReap {
				l.dead += t
				l.deadPriced += float64(t) * m.reach(r)
				l.deadMonth += float64(t * m.sessionsOf(r))
			}
		}
		for r, verb := range m.applied {
			count(r, verb)
		}
		for r, verb := range l.p.verbs {
			count(r, verb)
		}
		l.stats = map[int]stats{}
	}
	b.ok, b.mode, b.plat, b.applied, b.settled, b.version = true, l.mode, l.plat, len(m.applied), m.settled, l.p.version
}

func (l *list) statsOf(gn int) stats {
	st, ok := l.stats[gn]
	if !ok {
		st = l.m.groupStats(l.gs[gn].rows, l.p)
		l.stats[gn] = st
	}
	return st
}

// chosen is the rows a selection, or the cursor alone, acts on.
func (l *list) chosen(reach func(line) []*Row) []*Row {
	var rows []*Row
	for _, f := range l.flat[l.lo : l.hi+1] {
		rows = append(rows, reach(f)...)
	}
	return unique(rows)
}

func (l *list) toggle(items []*Row) {
	p := l.p
	p.refused = map[*Row]string{}
	var doable []*Row
	for _, r := range items {
		if best(r) != "" {
			doable = append(doable, r)
		}
	}
	if len(doable) == 0 {
		l.msg = "nothing unused here to select"
		if len(items) > 0 {
			l.msg = "nothing to select here: " + whyNothing(items[0])
			settled := true
			for _, r := range items {
				settled = settled && (r.Reason == reasonMuted || r.Reason == reasonPruned)
			}
			if settled {
				l.msg = "reap has nothing to change here: u undoes it"
			}
		}
		return
	}
	all := true
	for _, r := range doable {
		all = all && p.get(r) == best(r)
	}
	if all {
		for _, r := range doable {
			p.pop(r)
			delete(p.sweep, r)
		}
		l.msg = itoa(len(doable)) + " back"
		return
	}
	counts := map[string]int{}
	var order []string
	for _, r := range doable {
		p.set(r, best(r))
		p.sweep[r] = 0
		word := past[best(r)]
		if counts[word] == 0 {
			order = append(order, word)
		}
		counts[word]++
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	var said []string
	for _, word := range order {
		said = append(said, itoa(counts[word])+" "+word)
	}
	l.msg = strings.Join(said, " · ")
	if stuck := len(items) - len(doable); stuck > 0 {
		for _, r := range items {
			if best(r) == "" {
				l.msg += " · " + itoa(stuck) + " left: " + whyNot(r, vPrune)
				break
			}
		}
	}
}

func (l *list) act(items []*Row, ch rune) { l.msg = l.m.actOn(l.p, items, ch) }

// consume is the y having been carried out: every choice is spent.
func (l *list) consume() {
	l.p.clear()
	l.beforeOff = map[string]map[*Row]string{}
	l.forgetPaint()
	l.endSelection()
}

func (l *list) applyNow() string {
	// The cursor stays on its row when an apply moves it within the list, as a
	// lock does: the next key must not land on whatever took its place.
	if l.i >= 0 && l.i < len(l.flat) && !l.flat[l.i].isGroup {
		l.focus = l.flat[l.i].row
	}
	return l.m.receipt(l.p, l.unit, l.consume)
}

// ask puts a question on the status row and waits for y or n.
func (l *list) ask(question string) bool {
	m := l.m
	draw := func() bool {
		h, wd := m.s.Size()
		m.put(h-3, 3, strings.Repeat(" ", max(0, wd-6)), plain)
		m.put(h-3, 3, cut(question, max(0, wd-6)), asBold(m.tint(tintAmber)))
		m.s.Refresh()
		return true
	}
	return m.yesOrNo(draw, draw())
}

// mayQuit: choices not applied are lost with the window, so that takes an answer.
func (l *list) mayQuit() bool {
	if l.p.size() == 0 {
		return true
	}
	return l.ask("Quit and lose " + plural(l.p.size(), "choice") + "? [y/n]")
}

// pluginOff is o on any row or group of one plugin: the question is about the
// plugin, counted from all of its rows.
func (l *list) pluginOff(rows []*Row) string {
	m, p := l.m, l.p
	source, plug, ok := pluginTarget(rows)
	if !ok {
		return l.pluginsOff(rows)
	}
	var members []*Row
	for _, r := range m.pluginRows(source) {
		if m.applied[r] == "" {
			members = append(members, r)
		}
	}
	allOff := true
	for _, r := range members {
		allOff = allOff && p.get(r) == vDisable
	}
	if allOff {
		for _, r := range members {
			was := l.beforeOff[source][r]
			p.pop(r)
			if was != "" {
				p.set(r, was)
			}
		}
		p.takeEnded()
		l.msg = plug + " stays enabled"
		return l.msg
	}
	var used []string
	for _, r := range members {
		if r.Uses > 0 || r.Verdict == "KEEP" || r.Verdict == "REVIEW" || r.Reason == reasonMuted {
			used = append(used, r.Name[strings.LastIndex(r.Name, ":")+1:])
		}
	}
	if len(used) > 0 {
		names := strings.Join(used[:min(3, len(used))], ", ")
		question := "Disable " + plug + "? [y/N] all " + plural(len(members), "row") + " go, " + itoa(len(used)) +
			" you use, keep, muted or reap is unsure about: " + names
		if !l.ask(question) {
			l.msg = "cancelled"
			return l.msg
		}
	}
	before := map[*Row]string{}
	for _, r := range members {
		before[r] = p.get(r)
	}
	l.beforeOff[source] = before
	for _, r := range members {
		p.set(r, vDisable)
	}
	p.takeEnded()
	l.msg = plug + ": whole plugin will be disabled, " + plural(len(members), "row")
	return l.msg
}

// pluginsOff is o on a mixed group: every plugin in it, each asked about on its own.
func (l *list) pluginsOff(rows []*Row) string {
	several := pluginsIn(rows)
	in := map[string]bool{}
	for _, source := range several {
		in[source] = true
		l.pluginOff(l.m.pluginRows(source))
	}
	left, off := 0, 0
	for _, r := range rows {
		if !in[r.Source] {
			left++
		}
	}
	for _, source := range several {
		all := true
		for _, r := range l.m.pluginRows(source) {
			all = all && l.p.get(r) == vDisable
		}
		if all {
			off++
		}
	}
	switch {
	case len(several) > 0:
		l.msg = itoa(off) + " of " + plural(len(several), "plugin") + " will be disabled"
		if left > 0 {
			l.msg += " · " + itoa(left) + " not in a plugin"
		}
	default:
		l.msg = "o works on a plugin"
		if len(rows) > 0 {
			l.msg = "o cannot act here: " + whyNot(rows[0], vDisable)
		}
	}
	return l.msg
}

func (l *list) resetView() {
	l.i, l.opened, l.heldTop = 0, map[string]bool{}, -1
	l.endSelection()
}

// run is the list's loop: lay out, draw, read one key.
func (l *list) run() {
	m := l.m
	for {
		l.rebuild()
		hint := "q quits"
		if l.p.size() > 0 {
			hint = "q quits, once the window is large enough to ask about your choices"
		}
		if m.tooSmall(listRows, listCols, hint) {
			if k := m.s.ReadKey(-1); k == term.KeyEOF || (k == 'q' && l.p.size() == 0) {
				return
			}
			continue
		}
		if l.p.takeEnded() != "" {
			l.msg = strings.TrimLeft(l.msg+" · plugin off cancelled: it takes the whole plugin", " ·")
		}
		if len(l.flat) == 0 {
			if !l.emptyScreen() {
				return
			}
			continue
		}
		if l.focus != nil {
			for x, f := range l.flat {
				if !f.isGroup && f.row == l.focus {
					l.i = x
				}
			}
			l.focus = nil
		}
		l.i = max(0, min(l.i, len(l.flat)-1))
		l.lo, l.hi = l.i, l.i
		if l.anchor >= 0 {
			l.anchor = min(l.anchor, len(l.flat)-1)
			l.lo, l.hi = min(l.anchor, l.i), max(l.anchor, l.i)
		}
		moving := l.draw()
		wait := time.Duration(-1)
		switch {
		case moving:
			wait = frame
		case l.peek:
			wait = peekWait
		}
		k := m.s.ReadKey(wait)
		switch k {
		case term.KeyEOF:
			return
		case term.KeyNone:
			// The system can say the key is still down, where key repeat cannot be trusted.
			if !moving && !(l.peek && m.s.SHeld()) {
				l.peek = false
			}
			continue
		case term.KeyResize:
			continue
		case 's':
			l.peek = true
			continue
		}
		l.peek = false
		if !l.key(k) {
			return
		}
	}
}

// emptyScreen is the list with nothing in it. It returns false to leave.
func (l *list) emptyScreen() bool {
	m := l.m
	m.s.Erase()
	h, _ := m.s.Size()
	title := "nothing to review"
	if l.plat > 0 {
		title += " for " + l.platformLabel()
	}
	m.put(1, 3, title, bold)
	m.put(3, 3, l.msg, asBold(m.tint(tintGreen))) // what just emptied the list
	m.put(h-2, 3, "f platform · g group by · q", dim)
	m.s.Refresh()
	switch m.s.ReadKey(-1) {
	case 'q', term.KeyEOF:
		return false
	case 'f':
		l.plat = (l.plat + 1) % (len(m.platforms) + 1)
		l.resetView()
	case 'g':
		l.mode = (l.mode + 1) % 3
		l.resetView()
	}
	return true
}

// key handles one key on the list. It returns false to leave the menu.
func (l *list) key(k term.Key) bool {
	m := l.m
	switch k {
	case 'q':
		if l.mayQuit() {
			return false
		}
		l.msg = ""
		return true
	case term.KeyNonLatin:
		l.msg = latinHint
		return true
	case term.KeyMouse:
		l.mouse(m.s.Mouse())
		return true
	}
	l.forgetPaint()
	cur := l.flat[l.i]
	switch k {
	case 'o':
		l.repeat = nil
		if l.anchor >= 0 {
			l.msg = "o takes one plugin: esc ends the selection first"
			return true
		}
		l.pluginOff(groupAll(cur))
		return true
	case term.KeyTab:
		if m.price == 0 {
			l.msg = "no price known: tokens only"
			return true
		}
		if l.unit == unitMoney {
			l.unit = unitTokens
		} else {
			l.unit = unitMoney
		}
		return true
	case 'f':
		l.plat = (l.plat + 1) % (len(m.platforms) + 1)
		l.resetView()
		l.built.ok = false
		l.rebuild()
		l.shown, l.shownDead = float64(l.target), float64(l.dead)
		l.msg = "showing " + l.platformLabel()
		return true
	}
	var again []*Row
	if l.repeat != nil && l.repeat.key == k && l.anchor < 0 {
		again = l.repeat.items
	}
	wasSelecting := l.anchor >= 0 || again != nil
	l.repeat = nil
	l.keyOn(k, cur, again, wasSelecting)
	return !l.left
}

var pageKeys = map[term.Key]func(i, view, n int) int{
	term.KeyPgUp: func(i, view, _ int) int { return i - view },
	term.KeyPgDn: func(i, view, _ int) int { return i + view },
	term.KeyHome: func(_, _, _ int) int { return 0 },
	term.KeyEnd:  func(_, _, n int) int { return n - 1 },
}

// extend is a movement key: with Shift held it grows the selection, without it
// the selection ends, unless v is marking.
func (l *list) extend() {
	switch {
	case l.m.s.ShiftHeld():
		if l.anchor < 0 {
			l.anchor = l.i
		}
	case !l.marking:
		l.anchor = -1
	}
}

// keyOn is a key that acts on the cursor's line or on the selection.
func (l *list) keyOn(k term.Key, cur line, again []*Row, wasSelecting bool) {
	m := l.m
	reached := func() []*Row {
		if again != nil {
			return again
		}
		return l.chosen(groupAll)
	}
	switch {
	case k == term.KeyShiftUp || k == term.KeyShiftDown || k == 'K' || k == 'J':
		if l.anchor < 0 {
			l.anchor = l.i
		}
		if k == term.KeyShiftUp || k == 'K' {
			l.i--
		} else {
			l.i++
		}
	case k == 'v':
		l.marking = !l.marking
		l.anchor = -1
		if l.marking {
			l.anchor = l.i
		}
	case pageKeys[k] != nil:
		l.extend()
		l.i = pageKeys[k](l.i, l.view, len(l.flat))
		l.heldTop = -1
	case k == term.KeyUp || k == term.KeyDown:
		l.extend()
		if k == term.KeyUp {
			l.i--
		} else {
			l.i++
		}
	case k == term.KeyRight && cur.isGroup:
		l.opened[cur.name] = true
		l.endSelection() // the rows under a selection just changed
	case k == term.KeyLeft:
		delete(l.opened, cur.name)
		for x, f := range l.flat {
			if f.isGroup && f.gn == cur.gn {
				l.i = x
				break
			}
		}
		l.endSelection()
	case k == ' ':
		items := reached()
		l.toggle(items)
		l.endSelection()
		if wasSelecting {
			l.repeat = &repeat{k, items}
		}
	case term.IsApply(m.s, k, l.p.size() > 0):
		// Terminal sends Shift+Enter as plain Enter, same as Shift+arrows: the system is asked.
		l.msg = l.applyNow()
	case k == term.KeyShiftEnter || (k == term.KeyEnter && m.s.ShiftHeld()):
		l.msg = "nothing chosen yet: choose something, then " + term.ApplyKeyName(m.s) + " applies it"
	case k == term.KeyEnter && cur.isGroup:
		l.opened[cur.name] = !l.opened[cur.name]
		l.endSelection()
	case k == term.KeyEnter:
		l.openCard(cur)
	case k == 'p' || k == 'm' || k == 'l' || k == 'n' || k == 'd' || k == 'u':
		// Clearing reaches everything in the rows, also what a group action skipped.
		l.act(l.chosen(groupAll), rune(k))
		if k == 'd' {
			m.s.Beep()
		}
		l.endSelection()
	case k == 'g':
		l.mode = (l.mode + 1) % 3
		l.resetView()
		l.msg = modeNames[l.mode]
	case k == 'a':
		items := reached()
		l.p.refused = map[*Row]string{}
		l.msg = m.auto(l.p, items)
		l.endSelection()
		if wasSelecting {
			l.repeat = &repeat{k, items}
		}
	case k == term.KeyEsc:
		l.endSelection()
	}
}

func sameRows(a, b []*Row) bool {
	return len(a) == len(b) && (len(a) == 0 || a[0] == b[0])
}

// openCard shows the cursor's row on its own page, and comes back to the row the
// card ended on.
func (l *list) openCard(cur line) {
	m := l.m
	nextList := func(current []*Row, step int) []*Row {
		at := -1
		for x, g := range l.gs {
			if sameRows(g.rows, current) {
				at = x
			}
		}
		name, nextName := "", ""
		var following []*Row
		if at >= 0 {
			name = l.gs[at].name
			if other := at + step; other >= 0 && other < len(l.gs) {
				nextName, following = l.gs[other].name, l.gs[other].rows
			}
		}
		return m.sectionEnd(name, current, nextName, following, l.p, l.unitFmt, step < 0, l.applyNow)
	}
	was := m.s.SetMouse(false)
	stopped := m.card(cur.row, l.p, cur.rows, cardHooks{
		act:     l.act,
		onEnd:   func(c []*Row) []*Row { return nextList(c, 1) },
		onStart: func(c []*Row) []*Row { return nextList(c, -1) },
		onApply: l.applyNow,
		pending: l.p.size,
		onAuto: func(rows []*Row) string {
			var live []*Row
			for _, r := range rows {
				if m.applied[r] == "" {
					live = append(live, r)
				}
			}
			return m.auto(l.p, live)
		},
		onQuit:   l.mayQuit,
		onPlugin: func(r *Row) string { return l.pluginOff([]*Row{r}) },
	})
	m.s.SetMouse(was)
	if stopped == nil {
		l.left = true
		return
	}
	l.msg = "" // what the card did is on the row now: its last message may no longer be true
	for _, g := range l.gs {
		for _, r := range g.rows {
			if r == stopped {
				l.opened[g.name] = true
			}
		}
	}
	if stopped != cur.row {
		l.endSelection()
	}
	l.focus, l.heldTop = stopped, -1
}
