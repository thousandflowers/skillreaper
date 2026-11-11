package menu

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/term"
)

// Pick is one choice the receipt was shown and the user said yes to.
type Pick struct {
	Row  *Row
	Verb string
}

// Outcome is what became of a pick. Err is nil when it was carried out.
type Outcome struct {
	Pick Pick
	Err  error
}

// Applier carries choices out. It is the only thing that changes anything, and
// it is called once, after the receipt's y, with exactly what the receipt listed.
type Applier interface {
	Apply(picks []Pick) []Outcome
}

// Menu is one open menu: the data it was given and the screen it draws on.
type Menu struct {
	s     term.Screen
	apply Applier
	now   func() time.Time

	rep           *report.Report
	rows          []*Row // what the list shows
	all           []*Row // every row of the report, for the headline
	names         map[string]int
	warnings      []string
	platforms     []string // the platforms on the rows; filter 0 is all of them
	price         float64
	perMonth      int
	platMonth     map[string]int
	tokenSessions float64
	cur           Currency
	billing       string
	subscriber    bool
	// applied is what this session already carried out: out of the list, its
	// saving still counted.
	applied map[*Row]string
	settled int // how many applies this session has seen
}

// New prepares a menu over a report.
func New(s term.Screen, d Data, a Applier) *Menu {
	m := &Menu{s: s, apply: a, now: time.Now, rep: d.Report, cur: d.Currency, billing: term.Visible(d.Billing),
		names: map[string]int{}, applied: map[*Row]string{}}
	if m.cur.Sym == "" || m.cur.Rate <= 0 {
		m.cur = USD
	}
	m.subscriber = strings.Contains(m.billing, "subscription")
	for _, raw := range d.Report.Rows {
		r := cleanRow(raw)
		m.all = append(m.all, r)
		if listed(r.cat()) && !isOwn(r) {
			m.rows = append(m.rows, r)
		}
	}
	for _, e := range d.Pruned {
		m.rows = append(m.rows, prunedRow(e))
	}
	seen := map[string]bool{}
	for _, r := range m.rows {
		m.names[r.Name]++
		if !seen[r.Platform] {
			seen[r.Platform] = true
			m.platforms = append(m.platforms, r.Platform)
		}
	}
	sort.Strings(m.platforms)
	m.warnings = cleanWarnings(d.Report)
	m.perMonth = d.Report.SessionsPerMonth
	m.platMonth = d.Report.PlatformSessionsPerMonth
	if dead := d.Report.DeadTokensPerSession; dead > 0 {
		m.price = d.Report.MoneyPerMonth / float64(dead)
	}
	for _, r := range m.all {
		if r.Verdict == report.VerdictReap {
			m.tokenSessions += float64(r.Tokens * m.sessionsOf(r))
		}
	}
	return m
}

// cleanWarnings is what the report warns about, the ones that say the numbers
// are wrong first.
func cleanWarnings(rep *report.Report) []string {
	var wrong, advisory []string
	for _, w := range rep.Warnings {
		if w.Msg == "" {
			continue
		}
		text := w.Msg
		if w.Path != "" {
			text = filepath.Base(w.Path) + ": " + w.Msg
		}
		if w.Advisory {
			advisory = append(advisory, prose(text))
		} else {
			wrong = append(wrong, prose(text))
		}
	}
	return append(wrong, advisory...)
}

// displayName is a name, with its platform when two rows would otherwise read
// the same.
func (m *Menu) displayName(r *Row) string {
	if m.names[r.Name] > 1 {
		return r.Name + " (" + firstOf(r.Platform, unknownTool) + ")"
	}
	return r.Name
}

// wasted is the rows behind the headline: the report's own definition, so the
// menu and reap say one number.
func (m *Menu) wasted(visible func(*Row) bool) []*Row {
	var out []*Row
	for _, r := range m.all {
		if r.Verdict == report.VerdictReap && visible(r) {
			out = append(out, r)
		}
	}
	return out
}

// Run shows the menu until the user leaves it.
func (m *Menu) Run() {
	newList(m).run()
}

// settle records what an apply did, so the list tells the truth without reading
// the whole stack again: what went leaves the list and keeps its saving, a lock
// shows as locked.
// ponytail: an undone row is not weighed or judged again here, so it leaves the
// list too and is back at the next launch, when the scan reads it afresh. Reading
// the stack again after every apply would cost the whole scan each time.
func (m *Menu) settle(outcomes []Outcome) {
	for _, o := range outcomes {
		if o.Err != nil {
			continue
		}
		if o.Pick.Verb == vLock {
			o.Pick.Row.Kept = true
			continue
		}
		m.applied[o.Pick.Row] = o.Pick.Verb
	}
	m.settled++
}
