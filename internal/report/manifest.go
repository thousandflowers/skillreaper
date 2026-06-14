package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// Manifest is a small, shareable record a skill author can ship with a
// release: what it was tested against and how skillreaper observes it.
type Manifest struct {
	Skill             string        `json:"skill"`
	ClaudeCodeVersion string        `json:"claude_code_version"`
	InstallPath       string        `json:"install_path"`
	ToolSurface       string        `json:"tool_surface"`
	Hooks             []string      `json:"hooks"`
	RestorePath       string        `json:"restore_path"`
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
// locates the restore path; claudeVersion is best-effort ("" → "unknown").
// Returns false when the named skill is not in the inventory.
func BuildManifest(r *Report, skillName, claudeDir, claudeVersion string) (Manifest, bool) {
	row := findManifestSkill(r, skillName)
	if row == nil {
		return Manifest{}, false
	}
	if claudeVersion == "" {
		claudeVersion = "unknown"
	}

	var hooks []string
	for _, hr := range r.Rows {
		if hr.Category == scan.CatHook {
			hooks = append(hooks, hr.Description)
		}
	}

	surface := "all"
	if row.ToolSurface != scan.ToolSurfaceAll {
		surface = fmt.Sprintf("%d tool(s)", row.ToolSurface)
	}

	return Manifest{
		Skill:             row.Name,
		ClaudeCodeVersion: claudeVersion,
		InstallPath:       row.Path,
		ToolSurface:       surface,
		Hooks:             hooks,
		RestorePath:       filepath.Join(claudeDir, "reaped", "muted", manifestSanitize(row.Name)+".md.bak"),
		UsageWindow: ManifestUsage{
			Days:          r.WindowDays,
			Sessions:      r.Sessions,
			Uses:          row.Uses,
			Errors:        row.ErrorCount,
			Projects:      len(r.SkillProjects[row.Name]),
			LastUsed:      manifestTime(row.LastUsed),
			LastAttempted: manifestTime(row.LastAttempt),
			Verdict:       row.Verdict,
		},
	}, true
}

// findManifestSkill matches by exact key, then by bare suffix of a namespaced
// key (so "plan" matches "ecc:plan").
func findManifestSkill(r *Report, name string) *Row {
	for i := range r.Rows {
		if r.Rows[i].Category == scan.CatSkill && r.Rows[i].Name == name {
			return &r.Rows[i]
		}
	}
	for i := range r.Rows {
		rr := &r.Rows[i]
		if rr.Category != scan.CatSkill {
			continue
		}
		if j := strings.LastIndexByte(rr.Name, ':'); j >= 0 && rr.Name[j+1:] == name {
			return rr
		}
	}
	return nil
}

func manifestSanitize(name string) string {
	return strings.NewReplacer(":", "-", "/", "-", "\\", "-").Replace(name)
}

func manifestTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("2006-01-02")
}

// RenderManifestJSON writes the manifest as indented JSON.
func RenderManifestJSON(w io.Writer, m Manifest) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// RenderManifestMarkdown writes the manifest as a release-ready Markdown block.
func RenderManifestMarkdown(w io.Writer, m Manifest) {
	fmt.Fprintf(w, "## skillreaper manifest — `%s`\n\n", m.Skill)
	fmt.Fprintf(w, "- **Claude Code version tested:** %s\n", m.ClaudeCodeVersion)
	fmt.Fprintf(w, "- **Install path:** `%s`\n", m.InstallPath)
	fmt.Fprintf(w, "- **Tool surface:** %s\n", m.ToolSurface)
	fmt.Fprintf(w, "- **Restore path:** `%s`\n", m.RestorePath)
	if len(m.Hooks) > 0 {
		fmt.Fprintf(w, "- **Hooks present:**\n")
		for _, h := range m.Hooks {
			fmt.Fprintf(w, "  - `%s`\n", h)
		}
	} else {
		fmt.Fprintf(w, "- **Hooks present:** none\n")
	}
	u := m.UsageWindow
	fmt.Fprintf(w, "- **Observed usage (%dd):** %d sessions · %d uses · %d errors · %d project(s) · last used %s · verdict %s\n",
		u.Days, u.Sessions, u.Uses, u.Errors, u.Projects, u.LastUsed, u.Verdict)
}
