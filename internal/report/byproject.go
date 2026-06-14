package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// RenderByProject lists skills that fired, bucketed by the project (repo) they
// fired in. A skill concentrated in a single project — cold globally but hot
// locally — is flagged so it is not dismissed as merely cold.
func RenderByProject(w io.Writer, r *Report, color bool) {
	paint := painter(color)
	fmt.Fprintf(w, "\n  %s\n\n",
		paint(cBold, fmt.Sprintf("⟡ skills by project — last %d days · %d sessions", r.WindowDays, r.Sessions)))

	if len(r.SkillProjects) == 0 {
		fmt.Fprintf(w, "  %s\n\n", paint(cDim, "no skill firings attributed to a project."))
		return
	}

	type skillRow struct {
		skill    string
		projects map[string]int
	}
	rows := make([]skillRow, 0, len(r.SkillProjects))
	for s, p := range r.SkillProjects {
		rows = append(rows, skillRow{s, p})
	}
	// Concentrated skills (fewest projects) first — those are the ones a
	// global cold score would unfairly flag.
	sort.Slice(rows, func(i, j int) bool {
		if len(rows[i].projects) != len(rows[j].projects) {
			return len(rows[i].projects) < len(rows[j].projects)
		}
		return rows[i].skill < rows[j].skill
	})

	for _, sr := range rows {
		tag := ""
		if len(sr.projects) == 1 {
			tag = "  " + paint(cBYell, "⚑ repo-local")
		}
		fmt.Fprintf(w, "  %s  %s%s\n",
			paint(cBold, sr.skill),
			paint(cDim, fmt.Sprintf("%d project(s)", len(sr.projects))), tag)
		for _, pc := range sortedProjects(sr.projects) {
			fmt.Fprintf(w, "      %-50s %s\n",
				prettyProject(pc.name), paint(cDim, fmt.Sprintf("%d×", pc.count)))
		}
	}
	fmt.Fprintln(w)
}

type projCount struct {
	name  string
	count int
}

func sortedProjects(m map[string]int) []projCount {
	out := make([]projCount, 0, len(m))
	for n, c := range m {
		out = append(out, projCount{n, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].name < out[j].name
	})
	return out
}

// prettyProject turns Claude Code's encoded project dir ("-Users-me-repo")
// into a readable path. Lossy (dashes in names), so best-effort only.
func prettyProject(dir string) string {
	if strings.HasPrefix(dir, "-") {
		return strings.ReplaceAll(dir, "-", "/")
	}
	return dir
}

// RenderByProjectJSON writes the skill→project firing map as indented JSON.
func RenderByProjectJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r.SkillProjects)
}
