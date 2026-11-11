package menu

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/report"
)

// charsPerToken10 is internal/cost's estimate for a name, times ten: a muted
// item keeps its name.
const charsPerToken10 = 37

var (
	// lighter verbs take tokens off every session.
	lighter = map[string]bool{vPrune: true, vMute: true, vDisable: true, vDelete: true}
	// struck verbs take the row out of the stack, so its name is struck through;
	// a muted item stays, so it only fades.
	struck = map[string]bool{vPrune: true, vDelete: true, vDisable: true}
	letter = map[string]string{vPrune: "p", vMute: "m", vLock: "l", vDelete: "d", vDisable: "O", vUnmute: "u", vUnlock: "u", vRestore: "u"}
	past   = map[string]string{vPrune: "pruned", vMute: "muted", vLock: "locked", vDelete: "deleted", vUnmute: "undone", vUnlock: "undone", vRestore: "undone", "": "cleared"}
	verbS  = map[string]string{vPrune: "prunes", vMute: "mutes", vLock: "locks"}
)

// doneWords says what became of each verb, in the order the summary lists them.
var doneWords = [][2]string{
	{vPrune, "gone"}, {vDelete, "deleted"}, {vDisable, "off"}, {vMute, "muted"},
	{vLock, "locked"}, {vUnmute, "undone"}, {vUnlock, "undone"}, {vRestore, "undone"},
}

// actionGroups is every choice so far, added up by action in a fixed order.
var actionGroups = []struct {
	label string
	verbs []string
}{
	{"prune", []string{vPrune}}, {"mute", []string{vMute}}, {"delete", []string{vDelete}}, {"plugin off", []string{vDisable}},
	{"lock", []string{vLock}}, {"undo", []string{vUnmute, vUnlock, vRestore}},
}

// Plan is the choices by row. Any write forgets what `a` remembered for that
// row, so `a` can never undo a later choice. Plugin off is all or nothing: a row
// leaving it ends it for every row of that plugin, so box and receipt agree.
type Plan struct {
	verbs   map[*Row]string
	memory  map[*Row]string // what stood before reap's own choice; "" is nothing
	sweep   map[*Row]int    // how far the strike-through has run on a row
	refused map[*Row]string // the rows the last key left out, and the verb they refused
	version int
	ended   string
}

func newPlan() *Plan {
	return &Plan{verbs: map[*Row]string{}, memory: map[*Row]string{}, sweep: map[*Row]int{}, refused: map[*Row]string{}}
}

func (p *Plan) get(r *Row) string { return p.verbs[r] }

func (p *Plan) has(r *Row) bool { _, ok := p.verbs[r]; return ok }

func (p *Plan) size() int { return len(p.verbs) }

func (p *Plan) endPluginOff(r *Row, verb string) {
	if verb == vDisable || p.verbs[r] != vDisable {
		return
	}
	for other, v := range p.verbs {
		if v == vDisable && other.Source == r.Source && other != r {
			delete(p.memory, other)
			delete(p.verbs, other)
		}
	}
	p.ended = r.Source
}

// takeEnded is the plugin whose off a write just ended, once.
func (p *Plan) takeEnded() string {
	ended := p.ended
	p.ended = ""
	return ended
}

func (p *Plan) set(r *Row, verb string) {
	p.endPluginOff(r, verb)
	delete(p.memory, r)
	p.version++
	p.verbs[r] = verb
}

func (p *Plan) pop(r *Row) {
	p.endPluginOff(r, "")
	delete(p.memory, r)
	p.version++
	delete(p.verbs, r)
}

func (p *Plan) clear() {
	for r := range p.verbs {
		p.pop(r)
	}
	p.sweep = map[*Row]int{}
	p.takeEnded()
}

// saving is the tokens a choice really takes off every session. A muted item
// stays listed under its name.
func saving(r *Row, verb string) int {
	if verb == vMute {
		kept := (utf8.RuneCountInString(r.Name)*10 + charsPerToken10 - 1) / charsPerToken10
		return max(0, r.Tokens-kept)
	}
	if lighter[verb] {
		return r.Tokens
	}
	return 0
}

// stateBox says what you chose, with the letter of the key that sets it. A
// read-only row shows a box only once locked.
func stateBox(r *Row, p *Plan) string {
	if verb := p.get(r); verb != "" {
		return "[" + letter[verb] + "]"
	}
	if readOnly(r) {
		return "   "
	}
	return "[ ]"
}

// groupBox is [-] when mixed, [ ] only when nothing is chosen. A row reap has no
// quarrel with and you left alone does not make it mixed.
func groupBox(rows []*Row, p *Plan) string {
	marks := map[string]bool{}
	for _, r := range rows {
		if p.has(r) || (isDead(r) && !readOnly(r)) {
			marks[stateBox(r, p)] = true
		}
	}
	switch len(marks) {
	case 0:
		return "[ ]"
	case 1:
		for mark := range marks {
			return mark
		}
	}
	return "[-]"
}

func unique(rows []*Row) []*Row {
	seen := map[*Row]bool{}
	out := rows[:0:0]
	for _, r := range rows {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// actOn is a key on rows or a group: every row that can take it, and it names
// those that cannot. Returns what to say.
func (m *Menu) actOn(p *Plan, rows []*Row, ch rune) string {
	if ch == 'u' {
		n := 0
		for _, r := range rows {
			if undo := existingUndo[existing(r)]; undo != "" && m.applied[r] == "" {
				p.set(r, undo)
				p.sweep[r] = 0
				n++
			}
		}
		if n == 0 {
			return "nothing to undo here: u takes back a mute, a lock or a prune"
		}
		return itoa(n) + " undone"
	}
	verb := map[rune]string{'p': vPrune, 'm': vMute, 'l': vLock, 'd': vDelete, 'n': ""}[ch]
	n := 0
	var refused []*Row
	p.refused = map[*Row]string{}
	for _, r := range rows {
		if m.applied[r] != "" { // a card can still be showing a row that just went
			continue
		}
		switch {
		case verb == "" || verb == existing(r):
			p.pop(r) // n clears; a verb it already is goes back to that, as the card's arrows do
			n++
		case can(r, verb):
			p.set(r, verb)
			p.sweep[r] = 0
			n++
		default:
			refused = append(refused, r)
		}
	}
	msg := ""
	if n > 0 || len(refused) == 0 {
		msg = itoa(n) + " " + past[verb]
	}
	if verb == vDelete && n > 1 {
		msg = "⚠ " + itoa(n) + " marked for delete: cannot be restored"
	}
	if len(refused) > 0 {
		for _, r := range refused {
			p.refused[r] = verb
		}
		if msg != "" {
			msg += " · "
		}
		name := ""
		if len(refused) == 1 {
			name = refused[0].Name + ": "
		}
		msg += itoa(len(refused)) + " cannot be " + past[verb] + ": " + name + whyNot(refused[0], verb)
	}
	return msg
}

// auto sets what reap suggests, over any choice in the box; pressed again, it
// puts back what stood before that press. Where reap is not sure, the same
// choice a click takes, so a whole group can go at once. Nothing runs until apply.
func (m *Menu) auto(p *Plan, items []*Row) string {
	rows := unique(items)
	var picked []*Row
	picks := map[*Row]string{}
	left := 0
	for _, r := range rows {
		verb := ""
		if suggestion(r) != reasonMuted {
			verb = best(r)
		}
		if verb != "" {
			picks[r] = verb
			picked = append(picked, r)
		} else if s := suggestion(r); s != reasonMuted && s != suggestRO && !alreadyLocked(r) {
			left++
		}
	}
	tail := ""
	if left > 0 {
		tail = " · " + itoa(left) + " left: nothing reap can do to them"
	}
	if len(picks) == 0 {
		return "reap chose nothing" + tail
	}
	allMatch, remembered := true, false
	for _, r := range picked {
		allMatch = allMatch && p.get(r) == picks[r]
		_, has := p.memory[r]
		remembered = remembered || has
	}
	if allMatch && remembered {
		changed := false
		for _, r := range picked {
			was, has := p.memory[r]
			if !has {
				continue
			}
			changed = changed || was != p.get(r)
			p.pop(r)
			delete(p.sweep, r)
			if was != "" {
				p.set(r, was)
			}
		}
		if changed {
			return "reap's choice undone" + tail
		}
		return "nothing to undo" + tail
	}
	// Already what reap suggests with nothing else changing: there is nothing to
	// go back to, so a second `a` empties it.
	memory := map[*Row]string{}
	for _, r := range picked {
		memory[r] = ""
		if !allMatch {
			memory[r] = p.get(r)
		}
	}
	counts := map[string]int{}
	for _, r := range picked {
		p.pop(r)
		p.set(r, picks[r])
		p.sweep[r] = 0
		counts[picks[r]]++
	}
	for r, was := range memory {
		p.memory[r] = was
	}
	var said []string
	for _, verb := range []string{vPrune, vMute, vLock} {
		if counts[verb] > 0 {
			said = append(said, itoa(counts[verb])+" "+verb)
		}
	}
	return "reap chose: " + strings.Join(said, " · ") + tail
}

// planSummary is every choice so far, added up by action in a fixed order.
func planSummary(p *Plan) string {
	counts := map[string]int{}
	for _, verb := range p.verbs {
		counts[verb]++
	}
	var out []string
	for _, g := range actionGroups {
		n := 0
		for _, verb := range g.verbs {
			n += counts[verb]
		}
		if n > 0 {
			out = append(out, g.label+" "+itoa(n))
		}
	}
	return strings.Join(out, " · ")
}

var echo = regexp.MustCompile(`^(reap chose: )?\d+ \w+( · \d+ \w+)*$`)

// isEcho: a message that only repeats what the summary already shows; anything
// else is news and is shown instead.
func isEcho(msg string) bool { return echo.MatchString(msg) }

type stats struct {
	doable, marked           []*Row
	full, faded, allPruned   bool
	box, plugin              string
	muted, unused, deletes   int
	saved, total             int
	savedPriced, totalPriced float64
}

// groupStats is what a group row says: the rows you chose, out of all its rows.
func (m *Menu) groupStats(rows []*Row, p *Plan) stats {
	st := stats{box: groupBox(rows, p), plugin: pluginOf(rows), full: len(rows) > 0, faded: len(rows) > 0}
	for _, r := range rows {
		verb := p.get(r)
		st.full = st.full && struck[verb]
		st.faded = st.faded && lighter[verb]
		if r.Reason == reasonMuted {
			st.muted++
		}
		if r.Uses == 0 {
			st.unused++
		}
		if best(r) != "" {
			st.doable = append(st.doable, r)
			st.total += r.Tokens
			st.totalPriced += float64(r.Tokens) * m.reach(r)
		}
		if p.has(r) {
			st.marked = append(st.marked, r)
			st.saved += saving(r, verb)
			st.savedPriced += float64(saving(r, verb)) * m.reach(r)
			if verb == vDelete {
				st.deletes++
			}
		}
	}
	st.allPruned = len(st.doable) > 0
	for _, r := range st.doable {
		st.allPruned = st.allPruned && p.get(r) == best(r)
	}
	return st
}

// groupPhrase is a count that agrees with the rows under it: "unused" means no
// uses, and a muted row's uses prove nothing.
func (m *Menu) groupPhrase(rows []*Row, st stats) string {
	total := len(rows)
	if len(st.marked) > 0 {
		return m.shortCount(len(st.marked)) + " of " + m.shortCount(total) + " chosen"
	}
	if st.muted == total {
		return m.shortCount(total) + " muted"
	}
	pruned := total > 0
	for _, r := range rows {
		pruned = pruned && isPruned(r)
	}
	if pruned {
		return m.shortCount(total) + " pruned"
	}
	return m.shortCount(st.unused) + " of " + m.shortCount(total) + " unused"
}

// peekPhrase is what reap suggests inside a group, shown while s is held.
func peekPhrase(rows []*Row) string {
	counts := map[string]int{}
	var order []string
	for _, r := range rows {
		word := suggestionWord(r)
		if word == "" {
			word = "nothing"
		}
		if counts[word] == 0 {
			order = append(order, word)
		}
		counts[word]++
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	var out []string
	for _, word := range order[:min(2, len(order))] {
		out = append(out, itoa(counts[word])+" "+word)
	}
	return strings.Join(out, " · ")
}

// pointerTop: dragging onto the first or last visible row scrolls one row;
// otherwise the list stays under the pointer.
func pointerTop(top, rowY, yFirst, view, rows int) int {
	if rowY == yFirst && top > 0 {
		return top - 1
	}
	if rowY == yFirst+view-1 && top+view < rows {
		return top + 1
	}
	return top
}

// weigh is (tokens, the same weighed by each row's own sessions).
func (m *Menu) weigh(rows []*Row, tokens func(*Row) int) (int, float64) {
	tok, priced := 0, 0.0
	for _, r := range rows {
		t := tokens(r)
		tok += t
		priced += float64(t) * m.reach(r)
	}
	return tok, priced
}

func rowTokens(r *Row) int { return r.Tokens }

// verbsSaid is what space does to these rows, in one word when it is one thing.
func verbsSaid(rows []*Row) string {
	seen := map[string]bool{}
	var words []string
	for _, r := range rows {
		if w := verbS[best(r)]; w != "" && !seen[w] {
			seen[w] = true
			words = append(words, w)
		}
	}
	if len(words) == 0 {
		return "does nothing to"
	}
	sort.Strings(words)
	return strings.Join(words, " or ")
}

// line is one row of the list as it is laid out: a group header or an item.
type line struct {
	isGroup bool
	gn      int
	name    string
	row     *Row
	rows    []*Row // the group it is, or belongs to
}

// groupAll is what a line reaches for a choice that fits any row, like a lock or
// reap's own suggestion: everything in a group.
func groupAll(l line) []*Row {
	if !l.isGroup {
		return []*Row{l.row}
	}
	return l.rows
}

// rowItems is what a line acts on: the item, or a group's rows not worth
// locking; failing that all of them.
func rowItems(l line) []*Row {
	if !l.isGroup {
		return []*Row{l.row}
	}
	var out []*Row
	for _, r := range l.rows {
		if suggestion(r) != suggestKeep {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return l.rows
	}
	return out
}

func (m *Menu) cursorHint(l line, st stats, p *Plan) string {
	if l.isGroup {
		var hint string
		switch {
		case st.allPruned:
			hint = "space clears " + plural(len(st.doable), "item") + " in " + l.name
		case len(st.doable) > 0:
			hint = "space " + verbsSaid(st.doable) + " " + l.name + ": " + plural(len(st.doable), "item")
		default:
			hint = "reap has nothing to change in " + l.name + ": u undoes it"
		}
		if _, _, ok := pluginTarget(l.rows); ok {
			hint += " · o turns the whole plugin off"
		}
		return hint
	}
	r := l.row
	if best(r) == "" {
		switch reason := whyNot(r, vPrune); {
		case reason != "":
			return r.Name + " · " + reason
		case r.Reason == reasonMuted:
			return r.Name + " · it is muted: u unmutes it, p prunes it"
		}
		return r.Name + " · reap suggests nothing here: p, m, l and d still choose"
	}
	verb := verbS[best(r)]
	if p.get(r) == best(r) {
		verb = "clears"
	}
	hint := "space " + verb + " " + r.Name
	switch {
	case isPlugin(r):
		hint += " · o turns its plugin off"
	case can(r, vDelete):
		hint += " · d deletes it for good"
	}
	return hint
}

// sharedFolders: the folders a mute reaches beyond this tool. A linked skill's
// description lives there, for every tool using it.
func sharedFolders(picks []Pick) []string {
	var found []string
	for _, pk := range picks {
		if pk.Verb == vMute {
			found = append(found, sharedDir(pk.Row))
		}
	}
	return foldersSaid(found)
}

// removedForOthers: the shared folders a prune or a delete really takes
// something out of. Through a link, only the link goes.
func removedForOthers(picks []Pick) []string {
	var found []string
	for _, pk := range picks {
		if (pk.Verb == vPrune || pk.Verb == vDelete) && firstOf(pk.Row.LinkTarget, pk.Row.ViaLink) == "" {
			found = append(found, sharedDir(pk.Row))
		}
	}
	return foldersSaid(found)
}

// updateWarning: a plugin's update puts its files back. It only says so; the
// choice stays the user's.
func updateWarning(picks []Pick) string {
	n := 0
	for _, pk := range picks {
		if (pk.Verb == vPrune || pk.Verb == vDelete) && isPlugin(pk.Row) {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return "⚠ " + plural(n, "plugin item") + " may return with an update"
}

// usingWarning: chosen against the evidence. It only says so.
func usingWarning(picks []Pick) string {
	n := 0
	for _, pk := range picks {
		if lighter[pk.Verb] && inUse(pk.Row) {
			n++
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return "⚠ 1 of these is in use"
	}
	return "⚠ " + itoa(n) + " of these are in use"
}

// muteSavingAgrees is checked by the tests: the menu and the report must say one
// number for a mute.
func muteSavingAgrees(r *Row) bool { return saving(r, vMute) == report.MuteSaving(r.Row) }
