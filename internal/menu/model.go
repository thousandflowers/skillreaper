// Package menu is the interactive screen `skillreaper` opens: the list of what
// is loaded, a choice per row or group, a receipt, and only then the change.
// Nothing here touches the disk; an Applier does, after the receipt's y.
package menu

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/term"
)

// The verbs a pick carries, for whoever applies it.
const (
	Prune   = vPrune
	Mute    = vMute
	Lock    = vLock
	Delete  = vDelete
	Disable = vDisable
	Unmute  = vUnmute
	Unlock  = vUnlock
	Restore = vRestore
)

// The verbs a row can be given. The first four make a session lighter.
const (
	vPrune   = "prune"
	vMute    = "mute"
	vLock    = "lock"
	vDelete  = "delete"
	vDisable = "disable"
	vUnmute  = "unmute"
	vUnlock  = "unlock"
	vRestore = "restore"
)

const (
	ownPlugin     = "skillreaper"
	sourcePlugin  = "plugin:"
	sourcePruned  = "quarantine"
	reasonMuted   = "muted"
	reasonPruned  = "pruned"
	reasonKeep    = "keep"
	noName        = "(no name)"
	defaultTool   = "claude-code"
	unknownTool   = "unknown"
	suggestKeep   = "keep"
	suggestNone   = "-"
	suggestUnsure = "not sure"
	suggestPlugin = "/plugin"
	suggestRO     = "read-only"
)

var ownSkills = map[string]bool{"skillreaper": true, "reap": true}

// pluginID is what may be shown as a plugin, and later passed to a command.
var pluginID = regexp.MustCompile(`^[A-Za-z0-9][\w.-]*(@[A-Za-z0-9][\w.-]*)?$`)

// Row is one line of the list. The embedded row is cleaned for the screen; raw
// is exactly what was scanned, and the only thing an apply is given.
type Row struct {
	report.Row
	raw     report.Row
	PruneID string // the manifest id of a row that is in quarantine
}

// Raw is the row as it was scanned, for whoever carries a choice out.
func (r *Row) Raw() report.Row { return r.raw }

func (r *Row) cat() string { return string(r.Category) }

// Data is everything the menu is opened with.
type Data struct {
	Report   *report.Report
	Pruned   []prune.Entry // what is in quarantine now
	Billing  string        // the account's billing type, "" when unknown
	Currency Currency
}

// NewRow is a scanned row as the menu holds it.
func NewRow(raw report.Row) *Row { return cleanRow(raw) }

// PrunedRow is a quarantine entry as the menu holds it.
func PrunedRow(e prune.Entry) *Row { return prunedRow(e) }

func itoa(n int) string { return strconv.Itoa(n) }

// prose turns text from outside into one running line.
func prose(s string) string {
	var b strings.Builder
	for _, c := range s {
		if unicode.IsSpace(c) || unicode.In(c, unicode.Cc, unicode.Cf, unicode.Cs, unicode.Zl, unicode.Zp) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(c)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func cleanRow(raw report.Row) *Row {
	r := &Row{Row: raw, raw: raw}
	r.Name = term.Visible(raw.Name)
	if r.Name == "" {
		r.Name = noName
	}
	r.Platform = term.Visible(raw.Platform)
	r.Source = term.Visible(raw.Source)
	r.Reason = term.Visible(raw.Reason)
	r.Path = term.Visible(raw.Path)
	r.EvidenceNote = term.Visible(raw.EvidenceNote)
	r.Description = prose(raw.Description)
	r.Tokens = max(raw.Tokens, 0)
	r.Uses = max(raw.Uses, 0)
	r.ErrorCount = max(raw.ErrorCount, 0)
	return r
}

// prunedRow is a manifest entry as a list row: it left the stack, so it weighs
// nothing and nothing has used it since.
func prunedRow(e prune.Entry) *Row {
	var raw report.Row
	raw.Category = scan.CatSkill
	if e.Category != "" {
		raw.Category = scan.Category(e.Category)
	}
	raw.Name = e.Name
	raw.Platform = e.Platform
	if raw.Platform == "" {
		raw.Platform = defaultTool
	}
	raw.Source = sourcePruned
	raw.Path = e.To
	if raw.Path == "" {
		raw.Path = e.ID
	}
	raw.Verdict = report.VerdictInfo
	raw.Reason = reasonPruned
	r := cleanRow(raw)
	r.PruneID = e.ID
	return r
}

func isPlugin(r *Row) bool { return strings.HasPrefix(r.Source, sourcePlugin) }

func isPruned(r *Row) bool { return r.Reason == reasonPruned }

func isDead(r *Row) bool {
	return r.Verdict == report.VerdictReap || r.Verdict == report.VerdictMute
}

func listed(cat string) bool { return cat == "skill" || cat == "agent" || cat == "mcp" }

// readOnly: nothing can take it away. The file holds more than this row, or reap
// cannot edit it. It can only be locked.
func readOnly(r *Row) bool {
	if isPruned(r) {
		return false
	}
	return !listed(r.cat()) || (r.cat() == "mcp" && !r.Removable && !isPlugin(r))
}

// own is what a bulk reap prune takes, and so what reap suggests pruning.
func own(r *Row) bool {
	return r.Removable && !isPlugin(r) && !readOnly(r) && r.ViaLink == ""
}

// isOwn: reap never lists itself. A cleaner must not be able to clean itself
// away. Exact, so it hides nobody else's items.
func isOwn(r *Row) bool {
	if isPlugin(r) {
		id, _, _ := strings.Cut(r.Source[len(sourcePlugin):], "@")
		return strings.EqualFold(id, ownPlugin)
	}
	return r.cat() == "skill" && ownSkills[strings.ToLower(r.Name)]
}

func realpath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// linkLeadsOutside: mute edits through a link only into the folders reap knows;
// prune and delete take the link wherever it leads.
func linkLeadsOutside(r *Row) bool {
	if r.ViaLink == "" {
		return false
	}
	root := r.RootDir
	known := []string{filepath.Join(root, "skills"), filepath.Join(root, "plugins"), filepath.Join(root, "skills-reinvest"),
		filepath.Join(filepath.Dir(root), ".agents", "skills")}
	real := realpath(r.ViaLink)
	for _, k := range known {
		if strings.HasPrefix(real, realpath(k)+string(os.PathSeparator)) {
			return false
		}
	}
	return true
}

func byHand(r *Row) string {
	where := r.Path
	if where == "" {
		where = "its file"
	}
	return "reap cannot edit it: remove it by hand from " + where
}

// whyNot says why a row physically cannot take a verb, in one sentence; "" when
// it can. These are the only refusals there are.
func whyNot(r *Row, verb string) string {
	if isPruned(r) {
		return "it is pruned: u brings it back"
	}
	if alreadyLocked(r) && (verb == vPrune || verb == vDelete || verb == vMute) {
		return "it is locked: u unlocks it first"
	}
	switch verb {
	case vDisable:
		if isPlugin(r) {
			return ""
		}
		return "it is not in a plugin"
	case vLock:
		return ""
	case vPrune, vDelete:
		if r.FolderLink {
			return "the link it comes through is its whole skills folder: removing it would take every skill there"
		}
		if readOnly(r) {
			return byHand(r)
		}
		return ""
	}
	switch {
	case r.Reason == reasonMuted:
		return "it is muted already"
	case r.cat() == "mcp":
		return "an MCP server has nothing to mute, p prunes it"
	case readOnly(r):
		return byHand(r)
	case r.Description == "":
		return "it has no description to strip"
	case linkLeadsOutside(r):
		return "its link leads outside the folders reap may edit: p takes the link"
	}
	return ""
}

// can is what reap can carry out on this row. A pruned row is not in the stack:
// u brings it back, and then anything applies.
func can(r *Row, verb string) bool {
	return !isPruned(r) && whyNot(r, verb) == ""
}

func alreadyLocked(r *Row) bool { return r.Kept || r.Reason == reasonKeep }

// existing is what is already true of a row before any choice.
func existing(r *Row) string {
	switch {
	case r.Reason == reasonMuted:
		return vMute
	case r.Reason == reasonPruned:
		return vPrune
	case alreadyLocked(r):
		return vLock
	}
	return ""
}

// existingUndo: a state already on disk, and the verb that takes it off.
var existingUndo = map[string]string{vMute: vUnmute, vLock: vUnlock, vPrune: vRestore}

func suggestion(r *Row) string {
	switch {
	case isPruned(r):
		return reasonPruned
	case readOnly(r):
		return suggestRO
	case r.ViaLink != "" || !(r.Removable || isPlugin(r)):
		return suggestNone // a link or a shared folder: yours to choose, never reap's suggestion
	case r.Reason == reasonMuted:
		return reasonMuted
	case r.Verdict == report.VerdictReap && own(r):
		return vPrune
	case isDead(r) && (r.cat() == "skill" || r.cat() == "agent"):
		return vMute
	case r.Verdict == report.VerdictReap:
		return suggestPlugin
	case r.Verdict == report.VerdictKeep:
		return suggestKeep
	case r.Verdict == report.VerdictReview:
		return suggestUnsure
	}
	return suggestNone
}

// suggestionWord is what reap suggests, in the words the list uses.
func suggestionWord(r *Row) string {
	switch s := suggestion(r); s {
	case suggestKeep:
		return vLock
	case suggestNone:
		return ""
	default:
		return s
	}
}

var suggestLine = map[string]string{
	reasonMuted: "already muted", suggestUnsure: "reap is not sure", suggestNone: "reap suggests nothing",
	suggestRO:     "read-only: reap only weighs it",
	suggestPlugin: "reap suggests turning its plugin off: o, or p for this one alone",
}

func suggestionLine(r *Row) string {
	if line, ok := suggestLine[suggestion(r)]; ok {
		return line
	}
	return "reap suggests " + suggestionWord(r)
}

// suggestPlan is the choice each suggestion becomes.
var suggestPlan = map[string]string{vPrune: vPrune, vMute: vMute, suggestKeep: vLock}

// best is reap's own choice, what space, a click and a all take. On a row that
// is already muted or pruned it has none to make.
func best(r *Row) string {
	if r.Reason == reasonMuted || r.Reason == reasonPruned || alreadyLocked(r) {
		return ""
	}
	// Never a choice the row would refuse: space, a click and `a` set this
	// without asking again.
	if verb, ok := suggestPlan[suggestion(r)]; ok {
		if can(r, verb) {
			return verb
		}
		return ""
	}
	switch {
	case own(r) && can(r, vPrune):
		return vPrune
	case can(r, vMute):
		return vMute
	case can(r, vPrune):
		return vPrune
	}
	return ""
}

// inUse: chosen against the evidence. It fires, or reap itself would lock it.
func inUse(r *Row) bool { return r.Uses > 0 || suggestion(r) == suggestKeep }

var why = map[string]string{
	"unused":        "Never fired, with enough sessions to judge.",
	"broken":        "Invoked, and it only ever errored.",
	"heavy-rare":    "Used, but heavy and fired in too few sessions.",
	"tiny":          "Weighs almost nothing, not worth removing.",
	"grace":         "Installed too recently to judge.",
	"needs-data":    "Not enough sessions yet.",
	"no-transcript": "This platform keeps no transcripts, so there is no evidence.",
	"rare":          "Fired before this window, on a longer cadence.",
	"used":          "Used in this window.",
	"keep":          "You protected it with reap keep.",
	"claude-md-ref": "Named in your CLAUDE.md or rules.",
	"hook-loaded":   "Its plugin injects it every session.",
	"muted":         "You muted it, so its uses prove nothing.",
	"pruned":        "In quarantine: u brings it back.",
}

// whyLine is why reap says what it says about a row.
func whyLine(r *Row) string {
	if r.EvidenceNote != "" {
		return "No usage evidence: " + r.EvidenceNote + "."
	}
	if line, ok := why[r.Reason]; ok {
		return line
	}
	return r.Reason
}

// lastUsed reads a time as the reader's calendar days ago. A clock in the future
// is today.
func lastUsed(t, now time.Time) string {
	if t.IsZero() || t.Year() <= 1 {
		return "never"
	}
	y1, m1, d1 := now.Local().Date()
	y2, m2, d2 := t.Local().Date()
	today := time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)
	used := time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC)
	days := int(today.Sub(used).Hours() / 24)
	if days <= 0 {
		return "today"
	}
	return itoa(days) + "d ago"
}

// sourceName is where a row comes from, as a person would say it.
func sourceName(r *Row) string {
	s := r.Source
	switch {
	case isPlugin(r):
		id, _, _ := strings.Cut(s[len(sourcePlugin):], "@")
		return id
	case s == "personal":
		return "your own"
	case s == "user-config":
		return "your config"
	case strings.HasPrefix(s, "project:"):
		return "this project"
	}
	return s
}

type group struct {
	name string
	rows []*Row
}

// suggestGroups is the order the list opens in: what reap suggests first.
var suggestGroups = [][2]string{
	{vPrune, "suggest to prune"}, {vMute, "suggest to mute"}, {suggestUnsure, "not sure, you decide"},
	{suggestKeep, "suggest to lock"}, {reasonMuted, "already muted"}, {reasonPruned, "already pruned"},
}

// bySuggestion puts every row under what reap suggests for it. A row reap has no
// group for goes under "not sure", so every row stays reachable.
func (m *Menu) bySuggestion() []group {
	known := map[string]int{}
	out := make([]group, len(suggestGroups))
	for i, g := range suggestGroups {
		known[g[0]] = i
		out[i].name = g[1]
	}
	for _, r := range m.rows {
		at, ok := known[suggestion(r)]
		if !ok {
			at = known[suggestUnsure]
		}
		out[at].rows = append(out[at].rows, r)
	}
	return out
}

// bySource groups rows by where they come from, the heaviest unused first. Two
// sources that read the same stay apart: the marketplace or folder is named only
// when they collide.
func (m *Menu) bySource() []group {
	var order []string
	by := map[string][]*Row{}
	for _, r := range m.rows {
		if _, seen := by[r.Source]; !seen {
			order = append(order, r.Source)
		}
		by[r.Source] = append(by[r.Source], r)
	}
	taken := map[string]int{}
	for _, s := range order {
		taken[sourceName(by[s][0])]++
	}
	out := make([]group, 0, len(order))
	for _, s := range order {
		rows := by[s]
		label := sourceName(rows[0])
		if taken[label] > 1 {
			tail := filepath.Base(s)
			if strings.HasPrefix(s, sourcePlugin) {
				tail = s[strings.LastIndex(s, "@")+1:]
			}
			label += " · " + tail
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if isDead(rows[i]) != isDead(rows[j]) {
				return isDead(rows[i])
			}
			return rows[i].Tokens > rows[j].Tokens
		})
		out = append(out, group{label, rows})
	}
	dead := func(g group) int {
		n := 0
		for _, r := range g.rows {
			if isDead(r) {
				n += r.Tokens
			}
		}
		return n
	}
	sort.SliceStable(out, func(i, j int) bool { return dead(out[i]) > dead(out[j]) })
	return out
}

func (m *Menu) flatList() []group {
	rows := append([]*Row(nil), m.rows...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Tokens > rows[j].Tokens })
	return []group{{"everything", rows}}
}

var modeNames = [3]string{"by suggestion", "by plugin", "flat"}

// grouped is what g cycles through: by what reap suggests, by where it comes
// from, one flat list.
func (m *Menu) grouped(mode int) []group {
	switch mode {
	case 0:
		return m.bySuggestion()
	case 1:
		return m.bySource()
	}
	return m.flatList()
}

// pluginRows is every row of one plugin, wherever the list happens to show them.
func (m *Menu) pluginRows(source string) []*Row {
	var out []*Row
	for _, r := range m.rows {
		if r.Source == source {
			out = append(out, r)
		}
	}
	return out
}

// pluginTarget is the (source, id) of the one plugin these rows belong to, from
// any row or group of it.
func pluginTarget(rows []*Row) (source, id string, ok bool) {
	for i, r := range rows {
		if i == 0 {
			source = r.Source
		} else if r.Source != source {
			return "", "", false
		}
	}
	if !strings.HasPrefix(source, sourcePlugin) || !pluginID.MatchString(source[len(sourcePlugin):]) {
		return "", "", false
	}
	return source, source[len(sourcePlugin):], true
}

// pluginsIn is the plugins these rows belong to that o can name.
func pluginsIn(rows []*Row) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if _, _, ok := pluginTarget([]*Row{r}); ok && !seen[r.Source] {
			seen[r.Source] = true
			out = append(out, r.Source)
		}
	}
	sort.Strings(out)
	return out
}

// pluginOf is the plugin behind a group, or "" when the group is not one plugin
// with a valid id.
func pluginOf(rows []*Row) string {
	id := ""
	for _, r := range rows {
		if !isPlugin(r) {
			return ""
		}
		this := r.Source[len(sourcePlugin):]
		if id != "" && this != id {
			return ""
		}
		id = this
	}
	if !pluginID.MatchString(id) {
		return ""
	}
	return id
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// sharedDir is the folder this row's file really lives in, when other tools can
// be reading it too.
func sharedDir(r *Row) string {
	if link := firstOf(r.LinkTarget, r.ViaLink); link != "" {
		return filepath.Dir(link)
	}
	if r.cat() == "skill" && !r.Removable && !isPlugin(r) {
		return filepath.Dir(filepath.Dir(r.Path))
	}
	return ""
}

func foldersSaid(found []string) []string {
	home, _ := os.UserHomeDir()
	seen := map[string]bool{}
	var out []string
	for _, f := range found {
		if f == "" {
			continue
		}
		if home != "" && strings.HasPrefix(f, home+string(os.PathSeparator)) {
			f = "~" + f[len(home):]
		}
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}
