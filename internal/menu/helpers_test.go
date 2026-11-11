package menu

// What the tests of this package share: rows built as the prototype's test_menu.py
// builds them, a screen that remembers every frame, an applier that only records,
// and a clock that is never the real one.

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/term"
)

const (
	fooPlugin  = "plugin:foo@m"
	acmePlugin = "plugin:acme@market"
	testRows   = 30
	testCols   = 100
	settleTime = 60 // frames: more than the big number needs to land
)

// The keys the flows press, by what they are for.
const (
	up, down, left, right = term.KeyUp, term.KeyDown, term.KeyLeft, term.KeyRight
	enter, esc, tab       = term.KeyEnter, term.KeyEsc, term.KeyTab
	applyKey              = term.KeyShiftEnter
	space                 = term.Key(' ')
)

// opt changes one thing about a row built by raw.
type opt func(*report.Row)

// verdict sets the verdict as the prototype's row() does: an unused row says why.
func verdict(v string) opt {
	return func(r *report.Row) {
		r.Verdict, r.Reason = v, ""
		if v == report.VerdictReap {
			r.Reason = "unused"
		}
	}
}

func source(s string) opt          { return func(r *report.Row) { r.Source = s } }
func category(c scan.Category) opt { return func(r *report.Row) { r.Category = c } }
func tokens(n int) opt             { return func(r *report.Row) { r.Tokens = n } }
func uses(n int) opt               { return func(r *report.Row) { r.Uses = n } }
func reason(s string) opt          { return func(r *report.Row) { r.Reason = s } }
func platform(s string) opt        { return func(r *report.Row) { r.Platform = s } }
func notRemovable(r *report.Row)   { r.Removable = false }
func noDescription(r *report.Row)  { r.Description = "" }

// raw is the prototype's row(): an own skill nothing has used, 100 tokens heavy.
func raw(name string, opts ...opt) report.Row {
	var r report.Row
	r.Category, r.Name, r.Platform, r.Source, r.Path = scan.CatSkill, name, defaultTool, "personal", "/p/"+name
	r.Verdict, r.Reason, r.Tokens, r.Removable, r.Description = report.VerdictReap, "unused", 100, true, "d"
	for _, o := range opts {
		o(&r)
	}
	return r
}

// row is raw as the menu holds it.
func row(name string, opts ...opt) *Row { return cleanRow(raw(name, opts...)) }

// derive is a copy of a row with something changed, as {**row, ...} was.
func derive(r *Row, opts ...opt) *Row {
	changed := r.raw
	for _, o := range opts {
		o(&changed)
	}
	return cleanRow(changed)
}

// fixtureRows is the prototype's FIXTURE. Its "junk", its nameless row and its
// infinite weight have no Go form: a report.Row cannot hold them.
func fixtureRows() []report.Row {
	return []report.Row{
		raw("pruneme"), raw("muteme", verdict(report.VerdictMute)), raw("lockme", verdict(report.VerdictKeep)), raw("odd", verdict("WAT")),
		raw("srv", verdict(report.VerdictMute), tokens(500), category(scan.CatMCP)),
		raw("skillreaper:reap", source("plugin:skillreaper@m")), raw("skillreaper-notes"), raw("reap"), raw("cr\rSPOOF"),
	}
}

func fixtureReport() *report.Report {
	return &report.Report{Rows: fixtureRows(), DeadTokensPerSession: 1000, MoneyPerMonth: 1.0, Sessions: 50, WindowDays: 7}
}

// flowRows is a small stack with one row of every kind the flows need: two own
// unused items (one an MCP server), a plugin of two skills, a row reap keeps and
// a muted one. By suggestion it opens as prune (srv, old-notes), mute (acme:lint,
// acme:fmt), lock (daily), already muted (quiet), already pruned (gone).
func flowRows() []report.Row {
	return []report.Row{
		raw("old-notes"),
		raw("srv", category(scan.CatMCP), source("user-config"), tokens(500)),
		raw("acme:lint", source(acmePlugin), tokens(200), notRemovable),
		raw("acme:fmt", source(acmePlugin), tokens(150), notRemovable),
		raw("daily", verdict(report.VerdictKeep), reason("used"), uses(5), tokens(50)),
		raw("quiet", verdict(report.VerdictInfo), reason(reasonMuted), tokens(10)),
	}
}

// flowData has no price, so every amount on screen is in tokens.
func flowData() Data {
	rep := &report.Report{Sessions: 50, WindowDays: 7, SessionsPerMonth: 214, DeadTokensPerSession: 950, Rows: flowRows()}
	return Data{Report: rep, Pruned: []prune.Entry{{ID: "012", Name: "gone", Category: "skill", To: "/q/gone"}}}
}

// screen records every frame shown, so a test can read a question that the next
// frame painted over, and can change the window's size before a given key.
type screen struct {
	*term.Fake
	frames   [][]string
	mouseOn  []bool         // whether mouse reports were on, frame by frame
	resizeAt map[int][2]int // the size the window takes just before the nth key is read
	// watch is text whose style is noted the first time a frame shows it, on the
	// lowest line that holds it; styles is how each was drawn there.
	watch  []string
	styles map[string]term.Style
	reads  int
}

func (s *screen) Refresh() {
	s.Fake.Refresh()
	lines := s.Fake.Lines()
	s.frames = append(s.frames, lines)
	s.mouseOn = append(s.mouseOn, s.Fake.MouseOn)
	for _, text := range s.watch {
		if _, seen := s.styles[text]; seen {
			continue
		}
		for y := len(lines) - 1; y >= 0; y-- {
			if at := strings.Index(lines[y], text); at >= 0 {
				if s.styles == nil {
					s.styles = map[string]term.Style{}
				}
				s.styles[text] = s.Fake.StyleAt(y, utf8.RuneCountInString(lines[y][:at]))
				break
			}
		}
	}
}

func (s *screen) ReadKey(wait time.Duration) term.Key {
	s.reads++
	if size, ok := s.resizeAt[s.reads]; ok {
		s.Fake.Resize(size[0], size[1])
	}
	return s.Fake.ReadKey(wait)
}

// stepClock is a clock that moves on by step each time it is read.
func stepClock(step time.Duration) func() time.Time {
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		now = now.Add(step)
		return now
	}
}

// clockAt is a clock that reads each of these offsets in turn, then stays on the last.
func clockAt(offsets ...time.Duration) func() time.Time {
	start := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	read := 0
	return func() time.Time {
		at := offsets[min(read, len(offsets)-1)]
		read++
		return start.Add(at)
	}
}

// fakeApplier records the picks it is handed and fails the ones it was told to.
type fakeApplier struct {
	calls [][]Pick
	fail  map[string]error // by the row's scanned name
}

func (a *fakeApplier) Apply(picks []Pick) []Outcome {
	a.calls = append(a.calls, slices.Clone(picks))
	out := make([]Outcome, len(picks))
	for i, pk := range picks {
		out[i] = Outcome{Pick: pk, Err: a.fail[pk.Row.Raw().Name]}
	}
	return out
}

// said is the picks an applier was handed, as "verb name" in order.
func said(picks []Pick) []string {
	out := make([]string, len(picks))
	for i, pk := range picks {
		out[i] = pk.Verb + " " + pk.Row.Raw().Name
	}
	return out
}

// newMenu is a menu over a report, on a window large enough for every screen,
// under a HOME of its own and a clock on which every answer comes a second later.
func newMenu(t *testing.T, d Data) (*Menu, *screen, *fakeApplier) {
	t.Helper()
	return newMenuSized(t, testRows, testCols, d)
}

func newMenuSized(t *testing.T, rows, cols int, d Data) (*Menu, *screen, *fakeApplier) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s := &screen{Fake: term.NewFake(rows, cols)}
	a := &fakeApplier{}
	m := New(s, d, a)
	m.now = stepClock(time.Second)
	return m, s, a
}

// rulesMenu is a menu for the checks that need no rows of their own.
func rulesMenu(t *testing.T) *Menu {
	t.Helper()
	m, _, _ := newMenu(t, Data{Report: &report.Report{}})
	return m
}

func byName(t *testing.T, m *Menu) map[string]*Row {
	t.Helper()
	out := map[string]*Row{}
	for _, r := range m.rows {
		out[r.raw.Name] = r
	}
	return out
}

func picks(pairs ...any) []Pick {
	var out []Pick
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, Pick{pairs[i].(*Row), pairs[i+1].(string)})
	}
	return out
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("got %q, want it to contain %q", got, want)
	}
}

func mustEqual[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// session is one menu driven by fed keys until they run out.
type session struct {
	t *testing.T
	m *Menu
	s *screen
	a *fakeApplier
}

func open(t *testing.T, d Data) *session {
	t.Helper()
	return openSized(t, testRows, testCols, d)
}

func openSized(t *testing.T, rows, cols int, d Data) *session {
	t.Helper()
	m, s, a := newMenuSized(t, rows, cols, d)
	return &session{t, m, s, a}
}

// play opens the flow fixture, presses the keys and runs the menu until they run out.
func play(t *testing.T, keys ...term.Key) *session {
	t.Helper()
	return open(t, flowData()).press(keys...).run()
}

func (x *session) press(keys ...term.Key) *session {
	x.s.Feed(keys...)
	return x
}

// wait lets what is moving on screen land: frames go by with no key.
func (x *session) wait() *session {
	for range settleTime {
		x.s.Feed(term.KeyNone)
	}
	return x
}

func (x *session) click(col, line int) *session {
	x.s.FeedMouse(term.Mouse{X: col, Y: line, Kind: "press"})
	x.s.FeedMouse(term.Mouse{X: col, Y: line, Kind: "release"})
	return x
}

func (x *session) run() *session {
	x.m.Run()
	return x
}

// final is the last frame shown.
func (x *session) final() []string { return x.s.Lines() }

// frame is the first frame shown that holds every one of subs.
func (x *session) frame(subs ...string) []string {
	x.t.Helper()
	if at := x.frameAt(subs...); at >= 0 {
		return x.s.frames[at]
	}
	x.t.Fatalf("no frame shows all of %q; the last one was:\n%s", subs, strings.Join(x.final(), "\n"))
	return nil
}

func (x *session) frameAt(subs ...string) int {
	for at, f := range x.s.frames {
		all := true
		for _, sub := range subs {
			all = all && has(f, sub)
		}
		if all {
			return at
		}
	}
	return -1
}

func (x *session) saw(subs ...string) bool { return x.frameAt(subs...) >= 0 }

// applied is what the applier was handed, one "verb name" list per call.
func (x *session) applied() [][]string {
	var out [][]string
	for _, call := range x.a.calls {
		out = append(out, said(call))
	}
	return out
}

func has(lines []string, sub string) bool { return lineWith(lines, sub) != "" }

// lineWith is the first line that holds sub, or "". The line is given without
// its strike-through, which is a mark after every letter: isStruck asks about that.
func lineWith(lines []string, sub string) string {
	for _, l := range lines {
		if plain := strings.ReplaceAll(l, strikeRun, ""); strings.Contains(plain, sub) {
			return plain
		}
	}
	return ""
}

// isStruck says whether the line that holds sub is drawn struck through.
func isStruck(lines []string, sub string) bool {
	for _, l := range lines {
		if strings.Contains(strings.ReplaceAll(l, strikeRun, ""), sub) {
			return strings.Contains(l, strikeRun)
		}
	}
	return false
}

// statusLine is the row the list and the card speak on.
func statusLine(lines []string) string { return lines[len(lines)-3] }

// statusText is what the status row says, without the apply label at its right.
func statusText(lines []string) string {
	text, _, _ := strings.Cut(statusLine(lines), "shift+enter apply")
	return strings.TrimSpace(text)
}

func mustShow(t *testing.T, lines []string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if !has(lines, sub) {
			t.Errorf("the screen does not show %q:\n%s", sub, strings.Join(lines, "\n"))
		}
	}
}

func mustNotShow(t *testing.T, lines []string, subs ...string) {
	t.Helper()
	for _, sub := range subs {
		if l := lineWith(lines, sub); l != "" {
			t.Errorf("the screen shows %q: %q", sub, l)
		}
	}
}

func mustApply(t *testing.T, x *session, want ...[]string) {
	t.Helper()
	got := x.applied()
	if len(got) != len(want) {
		t.Fatalf("the applier was called %d times with %v, want %d: %v", len(got), got, len(want), want)
	}
	for i := range want {
		if !slices.Equal(got[i], want[i]) {
			t.Errorf("apply %d was handed %v, want %v", i+1, got[i], want[i])
		}
	}
}
