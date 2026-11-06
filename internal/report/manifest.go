package report

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// Manifest is a small, shareable record a skill author can ship with a
// release: what it was tested against and how skillreaper observes it.
type Manifest struct {
	Skill             string        `json:"skill"`
	Platform          string        `json:"platform"`
	ClaudeCodeVersion string        `json:"claude_code_version"`
	InstallPath       string        `json:"install_path"` // relative to the config root
	ToolSurface       string        `json:"tool_surface"`
	Hooks             []string      `json:"hooks"` // event and count, never the command
	UsageWindow       ManifestUsage `json:"usage_window"`
}

// ManifestUsage is the observed usage window for the skill.
type ManifestUsage struct {
	Days          int    `json:"days"`
	Sessions      int    `json:"sessions"`
	Uses          int    `json:"uses"`
	Errors        int    `json:"errors"`
	Projects      int    `json:"projects"`
	LastUsed      string `json:"last_used"`
	LastAttempted string `json:"last_attempted"`
	Verdict       string `json:"verdict"`
}

// BuildManifest assembles the manifest for one skill from the report. claudeDir
// is the root InstallPath is relative to when the row has none of its own;
// claudeVersion is best-effort ("" → "unknown"). This record ships with a
// release, so it carries no hook command, home path or local backup path.
// Returns false when the named skill is not in the inventory.
func BuildManifest(r *Report, skillName, claudeDir, claudeVersion string) (Manifest, bool) {
	row := findManifestSkill(r, skillName)
	if row == nil {
		return Manifest{}, false
	}
	if claudeVersion == "" {
		claudeVersion = "unknown"
	}

	surface := "all"
	if row.ToolSurface != scan.ToolSurfaceAll {
		surface = fmt.Sprintf("%d tool(s)", row.ToolSurface)
	}

	return Manifest{
		Skill:             row.Name,
		Platform:          row.Platform,
		ClaudeCodeVersion: claudeVersion,
		InstallPath:       manifestPath(*row, claudeDir),
		ToolSurface:       surface,
		Hooks:             hookEvents(r.Rows),
		UsageWindow: ManifestUsage{
			Days:          r.WindowDays,
			Sessions:      r.Sessions,
			Uses:          row.Uses,
			Errors:        row.ErrorCount,
			Projects:      manifestProjects(r, row.Item),
			LastUsed:      manifestTime(row.LastUsed),
			LastAttempted: manifestTime(row.LastAttempt),
			Verdict:       row.Verdict,
		},
	}, true
}

// hookEvents lists each hook event with how many hooks it runs, sorted.
func hookEvents(rows []Row) []string {
	counts := map[string]int{}
	for _, hr := range rows {
		if hr.Category == scan.CatHook {
			event, _, _ := strings.Cut(hr.Name, "#")
			counts[event]++
		}
	}
	out := make([]string, 0, len(counts))
	for event, n := range counts {
		out = append(out, fmt.Sprintf("%s (%d)", event, n))
	}
	sort.Strings(out)
	return out
}

func manifestPath(row Row, claudeDir string) string {
	root := row.RootDir
	if root == "" {
		root = claudeDir
	}
	if rel, err := filepath.Rel(root, row.Path); err == nil && !strings.HasPrefix(rel, "..") {
		return scan.SafeText(rel)
	}
	return scan.SafeText(filepath.Base(filepath.Dir(row.Path)) + "/" + filepath.Base(row.Path))
}

// manifestProjects counts projects across every name the skill fired under, as
// the use count does.
func manifestProjects(r *Report, it scan.Item) int {
	projects := map[string]bool{}
	for _, key := range rawKeys(it) {
		for p := range r.SkillProjects[key] {
			projects[p] = true
		}
	}
	return len(projects)
}

// findManifestSkill matches by exact key, then by bare suffix of a namespaced
// key (so "plan" matches "ecc:plan"). When several platforms hold the name, the
// copy with the most uses wins, and Manifest.Platform says which one it is.
func findManifestSkill(r *Report, name string) *Row {
	pick := func(match func(Row) bool) *Row {
		var best *Row
		for i := range r.Rows {
			rr := &r.Rows[i]
			if rr.Category == scan.CatSkill && match(*rr) && (best == nil || rr.Uses > best.Uses) {
				best = rr
			}
		}
		return best
	}
	if row := pick(func(rr Row) bool { return rr.Name == name }); row != nil {
		return row
	}
	return pick(func(rr Row) bool { return bareSuffix(rr.Name) == name })
}

func manifestTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02")
}

// RenderManifestJSON writes the manifest as indented JSON.
func RenderManifestJSON(w io.Writer, m Manifest) error {
	return WriteJSON(w, m)
}

// RenderManifestMarkdown writes the manifest as a release-ready Markdown block.
func RenderManifestMarkdown(w io.Writer, m Manifest) {
	fmt.Fprintf(w, "## skillreaper manifest — `%s`\n\n", mdCell(m.Skill))
	fmt.Fprintf(w, "- **Platform:** %s\n", m.Platform)
	fmt.Fprintf(w, "- **Claude Code version tested:** %s\n", m.ClaudeCodeVersion)
	fmt.Fprintf(w, "- **Install path:** `%s`\n", mdCell(m.InstallPath))
	fmt.Fprintf(w, "- **Tool surface:** %s\n", m.ToolSurface)
	if len(m.Hooks) > 0 {
		fmt.Fprintf(w, "- **Hooks present:**\n")
		for _, h := range m.Hooks {
			fmt.Fprintf(w, "  - %s\n", mdCell(h))
		}
	} else {
		fmt.Fprintf(w, "- **Hooks present:** none\n")
	}
	u := m.UsageWindow
	fmt.Fprintf(w, "- **Observed usage (%dd):** %d sessions · %d uses · %d errors · %d project(s) · last used %s · verdict %s\n",
		u.Days, u.Sessions, u.Uses, u.Errors, u.Projects, u.LastUsed, u.Verdict)
}
