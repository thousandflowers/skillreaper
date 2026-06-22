package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// APM manifest emission ("reap apm") — turn per-repo firing evidence into an
// Agent Package Manager (github.com/microsoft/apm) apm.yml.
//
// skillreaper already knows what actually fired per repo (SkillProjects). APM's
// unit of config is a per-repo apm.yml declaring what should be installed. Those
// are two ends of the same loop, but an apm.yml is normally hand-curated and the
// author guesses. This command derives it from evidence — read-only: it emits
// YAML to stdout and never edits the repo or runs `apm install`.
//
// Verdicts drive selection: KEEP/used → include, REAP → omit, REVIEW → never
// auto-omit (incomplete evidence never flags an item). First cut: skills only
// (the strongest firing signal); MCP servers and agents are follow-ups.
//
// Identity is the hard part: skillreaper knows a skill by local name; APM
// declares it by upstream coordinates (e.g. "anthropics/skills/skills/frontend-design").
// We recover coordinates from the repo's apm.lock.yaml when present; otherwise we
// emit a clearly-marked placeholder rather than inventing coordinates.

// apmCoordRe matches an APM-style coordinate token: at least owner/repo/path,
// optionally version-pinned with "#tag". Used to harvest declared coordinates
// from an existing apm.yml / apm.lock.yaml tolerantly, regardless of nesting.
var apmCoordRe = regexp.MustCompile(`[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+(?:#[A-Za-z0-9_.-]+)?`)

// APM diff statuses.
const (
	apmStatusDeclared = "declared" // fired here and already declared
	apmStatusAdd      = "add"      // fired here but not declared → suggest adding
	apmStatusDrop     = "drop"     // declared but cold here → suggest dropping
)

// APMDep is one skill dependency in the proposed manifest.
type APMDep struct {
	Name        string `json:"name"`             // local skill name
	Coordinate  string `json:"coordinate"`       // upstream owner/repo/path, or "" if unknown
	Placeholder bool   `json:"placeholder"`      // coordinate could not be resolved
	Source      string `json:"source"`           // provenance class (scan.Item.Source)
	Uses        int    `json:"uses"`             // firings in this repo within the window
	Status      string `json:"status,omitempty"` // diff mode only
}

// APMReport is the proposed manifest plus optional reconciliation against an
// existing apm.yml.
type APMReport struct {
	Repo       string   `json:"repo"`
	WindowDays int      `json:"window_days"`
	Sessions   int      `json:"sessions"`
	Deps       []APMDep `json:"dependencies"`
	Diff       bool     `json:"diff"`
	DiffPath   string   `json:"diff_path,omitempty"`
	Drop       []APMDep `json:"drop,omitempty"` // declared but cold here
}

// BuildAPM derives the proposed manifest from what fired in cwd's repo bucket.
// lock maps a local skill name (or its bare suffix) to a recovered upstream
// coordinate. declared (nil in propose mode) is the set of coordinate
// last-segments already declared in the --diff target; when non-nil, BuildAPM
// annotates each dep with a diff status and computes drop suggestions.
func BuildAPM(r *Report, cwd string, lock map[string]string, declared map[string]bool, diffPath string) APMReport {
	repoKey := encodeProject(cwd)
	out := APMReport{
		Repo:       cwd, // verbatim — the encoded key is lossy and not round-trippable
		WindowDays: r.WindowDays,
		Sessions:   r.Sessions,
		Diff:       declared != nil,
		DiffPath:   diffPath,
	}

	// reviewProtected holds names of REVIEW skills so the diff never suggests
	// dropping a declared entry whose evidence is merely incomplete.
	reviewProtected := map[string]bool{}

	for _, row := range r.Rows {
		if row.Category != scan.CatSkill {
			continue // first cut: skills only
		}
		if row.Verdict == VerdictReview {
			reviewProtected[row.Name] = true
			if s := bareSuffix(row.Name); s != "" {
				reviewProtected[s] = true
			}
		}
		if row.Verdict == VerdictReap {
			continue // dead weight — omit from the manifest
		}
		uses := repoUses(r, row.Name, repoKey)
		if uses == 0 {
			continue // didn't fire in this repo within the window
		}
		coord, placeholder := resolveCoordinate(row.Name, lock)
		dep := APMDep{
			Name:        row.Name,
			Coordinate:  coord,
			Placeholder: placeholder,
			Source:      row.Source,
			Uses:        uses,
		}
		if declared != nil {
			if declaredMatches(dep, declared) {
				dep.Status = apmStatusDeclared
			} else {
				dep.Status = apmStatusAdd
			}
		}
		out.Deps = append(out.Deps, dep)
	}

	sort.SliceStable(out.Deps, func(i, j int) bool {
		if out.Deps[i].Uses != out.Deps[j].Uses {
			return out.Deps[i].Uses > out.Deps[j].Uses
		}
		return out.Deps[i].Name < out.Deps[j].Name
	})

	if declared != nil {
		out.Drop = computeDrop(out.Deps, declared, reviewProtected)
	}
	return out
}

// computeDrop returns the declared coordinate segments that no surviving,
// fired-here skill accounts for — candidates to drop — excluding any protected
// by a REVIEW verdict (incomplete evidence is never grounds to drop).
func computeDrop(deps []APMDep, declared, reviewProtected map[string]bool) []APMDep {
	covered := map[string]bool{}
	for _, d := range deps {
		for _, k := range depKeys(d) {
			covered[k] = true
		}
	}
	var drop []APMDep
	for name := range declared {
		if covered[name] || reviewProtected[name] {
			continue
		}
		drop = append(drop, APMDep{Name: name, Status: apmStatusDrop})
	}
	sort.SliceStable(drop, func(i, j int) bool { return drop[i].Name < drop[j].Name })
	return drop
}

// resolveCoordinate recovers an upstream coordinate for a local skill from the
// lockfile, by full name then bare suffix. Absent that evidence it returns a
// clearly-marked placeholder — we never invent coordinates we cannot verify.
func resolveCoordinate(name string, lock map[string]string) (coord string, placeholder bool) {
	if c := lock[name]; c != "" {
		return c, false
	}
	if s := bareSuffix(name); s != "" {
		if c := lock[s]; c != "" {
			return c, false
		}
	}
	return "", true
}

// declaredMatches reports whether a dep is already declared, matching on its
// coordinate's last segment or the skill's (bare) name.
func declaredMatches(d APMDep, declared map[string]bool) bool {
	for _, k := range depKeys(d) {
		if declared[k] {
			return true
		}
	}
	return false
}

// depKeys returns the lookup keys a dep can be matched by: its name, bare
// suffix, and coordinate last-segment.
func depKeys(d APMDep) []string {
	keys := []string{d.Name}
	if s := bareSuffix(d.Name); s != "" {
		keys = append(keys, s)
	}
	if d.Coordinate != "" {
		keys = append(keys, coordLastSegment(d.Coordinate))
	}
	return keys
}

// LoadAPMManifest tolerantly harvests declared coordinates from an apm.yml or
// apm.lock.yaml. It returns the set of coordinate last-segments (for matching)
// and a name→coordinate map (for coordinate recovery). A missing file yields
// (nil, nil, nil); other read errors are returned.
func LoadAPMManifest(path string) (declared map[string]bool, lock map[string]string, err error) {
	if path == "" {
		return nil, nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	declared = map[string]bool{}
	lock = map[string]string{}
	for _, raw := range strings.Split(string(data), "\n") {
		// Only dependency list items carry coordinates. Skipping non-list lines
		// keeps slashes in comments, the "# repo:" header, URLs, and the literal
		// "owner/repo/path" placeholder hint out of the declared/lock sets.
		line := stripYAMLComment(raw)
		if !strings.HasPrefix(strings.TrimSpace(line), "-") {
			continue
		}
		for _, m := range apmCoordRe.FindAllString(line, -1) {
			seg := coordLastSegment(m)
			if seg == "" {
				continue
			}
			declared[seg] = true
			if lock[seg] == "" { // first real declaration wins
				lock[seg] = m
			}
		}
	}
	return declared, lock, nil
}

// stripYAMLComment removes a trailing YAML comment from a line. A '#' counts as
// a comment only outside quotes and at line start or after whitespace, so quoted
// or unquoted "#version" pins inside a coordinate are preserved.
func stripYAMLComment(line string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
				return line[:i]
			}
		}
	}
	return line
}

// LoadAPMContext gathers everything BuildAPM needs from disk: the declared
// coordinate set (diff mode only, non-nil when diffPath is set) and a
// name→coordinate lock map recovered from the diff file and/or a sibling
// apm.lock.yaml. Filesystem-facing entry point for the CLI.
func LoadAPMContext(diffPath, cwd string) (declared map[string]bool, lock map[string]string, err error) {
	lock = map[string]string{}
	if diffPath != "" {
		d, l, e := LoadAPMManifest(diffPath)
		if e != nil {
			return nil, nil, e
		}
		// Present-but-empty marks diff mode even when the target declares nothing.
		declared = d
		if declared == nil {
			declared = map[string]bool{}
		}
		for k, v := range l {
			lock[k] = v
		}
	}
	if sib := apmSiblingPath(diffPath, cwd); sib != "" {
		_, l, e := LoadAPMManifest(sib)
		if e != nil {
			return nil, nil, e
		}
		for k, v := range l {
			if lock[k] == "" {
				lock[k] = v
			}
		}
	}
	return declared, lock, nil
}

// apmSiblingPath locates an apm.lock.yaml to recover coordinates from: next to
// the --diff target first, then in cwd. Returns "" if neither exists.
func apmSiblingPath(diffPath, cwd string) string {
	var candidates []string
	if diffPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(diffPath), "apm.lock.yaml"))
	}
	if cwd != "" {
		candidates = append(candidates, filepath.Join(cwd, "apm.lock.yaml"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// repoUses sums a skill's firings in one repo bucket, under its full name and
// bare slash-command suffix (mirrors lookupUses aliasing).
func repoUses(r *Report, name, repoKey string) int {
	if repoKey == "" {
		return 0
	}
	n := r.SkillProjects[name][repoKey]
	if s := bareSuffix(name); s != "" {
		n += r.SkillProjects[s][repoKey]
	}
	return n
}

// projectEncodeRe matches every character Claude Code collapses when it names a
// ~/.claude/projects bucket from a cwd: all non-alphanumerics (/, ., _, space,
// existing '-', …), each replaced by a single '-'.
var projectEncodeRe = regexp.MustCompile(`[^A-Za-z0-9]`)

// encodeProject reproduces Claude Code's project-bucket directory name for a
// cwd, so a repo path matches the key stored in usage.Stats.SkillProjects.
// CC replaces every non-alphanumeric run-member with '-'; matching that exactly
// is what makes the per-repo firing lookup work for paths with '.', '_', or
// spaces (e.g. ".config", "my_project", "Application Support").
func encodeProject(cwd string) string {
	if cwd == "" {
		return ""
	}
	return projectEncodeRe.ReplaceAllString(cwd, "-")
}

// bareSuffix returns the part of a namespaced skill name after the last ':',
// or "" when there is no namespace.
func bareSuffix(name string) string {
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// coordLastSegment returns the final path segment of a coordinate, with any
// "#version" pin removed: "anthropics/skills/skills/frontend-design#v1" → "frontend-design".
func coordLastSegment(coord string) string {
	if i := strings.IndexByte(coord, '#'); i >= 0 {
		coord = coord[:i]
	}
	coord = strings.TrimRight(coord, "/")
	if i := strings.LastIndexByte(coord, '/'); i >= 0 {
		return coord[i+1:]
	}
	return coord
}

// RenderAPMYAML emits the proposed apm.yml. Resolved skills become active
// dependency lines; unresolved ones become TODO comments so the file is valid
// and never carries an invented coordinate. Diff suggestions are appended as
// comments.
func RenderAPMYAML(w io.Writer, m APMReport) {
	fmt.Fprintln(w, "# apm.yml — proposed by skillreaper from firing evidence (not applied)")
	fmt.Fprintf(w, "# repo: %s · window: %dd · %d sessions\n", orNone(m.Repo), m.WindowDays, m.Sessions)
	fmt.Fprintln(w, "# skills only (first cut); MCP servers and agents are not emitted yet.")
	fmt.Fprintln(w, "dependencies:")
	// Emit "apm: []" when no line will be an active list item, so the value
	// parses as an empty list rather than null (comments don't make a list).
	hasActive := false
	for _, d := range m.Deps {
		if !d.Placeholder {
			hasActive = true
			break
		}
	}
	if hasActive {
		fmt.Fprintln(w, "  apm:")
	} else {
		fmt.Fprintln(w, "  apm: []")
	}
	if len(m.Deps) == 0 {
		fmt.Fprintln(w, "    # no skills fired in this repo within the window.")
	}
	for _, d := range m.Deps {
		if d.Placeholder {
			fmt.Fprintf(w, "    # TODO(skillreaper): unknown upstream for %q (%d× here) — set owner/repo/path\n", d.Name, d.Uses)
			continue
		}
		fmt.Fprintf(w, "    - %q%s\n", d.Coordinate, apmInlineNote(d))
	}
	if m.Diff {
		renderAPMDiffComments(w, m)
	}
}

// apmInlineNote is the trailing "# Nx here · status" annotation on a dep line.
func apmInlineNote(d APMDep) string {
	note := fmt.Sprintf("   # %d× here", d.Uses)
	if d.Status == apmStatusAdd {
		note += " · not declared → add"
	}
	return note
}

// renderAPMDiffComments appends the reconcile summary as YAML comments.
func renderAPMDiffComments(w io.Writer, m APMReport) {
	fmt.Fprintf(w, "# --- reconcile vs %s ---\n", m.DiffPath)
	var adds []string
	for _, d := range m.Deps {
		if d.Status == apmStatusAdd {
			adds = append(adds, d.Name)
		}
	}
	if len(adds) > 0 {
		fmt.Fprintf(w, "#   + add (fired here, not declared): %s\n", strings.Join(adds, ", "))
	}
	if len(m.Drop) > 0 {
		var drops []string
		for _, d := range m.Drop {
			drops = append(drops, d.Name)
		}
		fmt.Fprintf(w, "#   - drop (declared, cold here): %s\n", strings.Join(drops, ", "))
	}
	if len(adds) == 0 && len(m.Drop) == 0 {
		fmt.Fprintln(w, "#   in sync — nothing to add or drop.")
	}
}

// RenderAPMJSON writes the report as indented JSON.
func RenderAPMJSON(w io.Writer, m APMReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// RenderAPMMarkdown writes the report as Markdown.
func RenderAPMMarkdown(w io.Writer, m APMReport) {
	fmt.Fprintf(w, "# apm.yml proposal — %s\n\n", orNone(m.Repo))
	fmt.Fprintf(w, "_From firing evidence over %d days · %d sessions · skills only._\n\n", m.WindowDays, m.Sessions)
	fmt.Fprintln(w, "| Skill | Coordinate | Uses here | Status |")
	fmt.Fprintln(w, "|---|---|---|---|")
	for _, d := range m.Deps {
		coord := d.Coordinate
		if d.Placeholder {
			coord = "_(unknown — set owner/repo/path)_"
		}
		status := d.Status
		if status == "" {
			status = "include"
		}
		fmt.Fprintf(w, "| %s | %s | %d | %s |\n", d.Name, coord, d.Uses, status)
	}
	if len(m.Drop) > 0 {
		fmt.Fprintf(w, "\n## Declared but cold here (suggest dropping)\n\n")
		for _, d := range m.Drop {
			fmt.Fprintf(w, "- %s\n", d.Name)
		}
	}
}

// orNone renders an empty string as "(unknown)".
func orNone(s string) string {
	if s == "" {
		return "(unknown)"
	}
	return s
}
