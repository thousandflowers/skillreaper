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
	width := termWidth(w)
	fmt.Fprintf(w, "\n  %s\n", paint(cCyan,
		rule(fmt.Sprintf("skills by project · last %d days · %d sessions", r.WindowDays, r.Sessions), width-2)))

	if len(r.SkillProjects) == 0 {
		fmt.Fprintf(w, "\n  %s\n\n", paint(cDim, "no skill firings attributed to a project."))
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

	// One flat table, a row per skill-and-project pair.
	//
	// It used to be a nested list: a bold skill heading, then its projects
	// indented under it. With most skills firing in exactly one project that
	// spent three lines saying what one line says, and it made the only
	// comparison worth making — which skills fire most, and where — impossible,
	// because the counts sat at a different depth from the names and never
	// lined up in a column.
	//
	// "repo-local" was also a yellow flag. It is the whole point of this view,
	// a skill that looks cold globally only because it is hot in exactly one
	// repo, so it is a word in its own column now and survives a pipe.
	var skills, projects, counts, notes []string
	for _, sr := range rows {
		note := ""
		if len(sr.projects) == 1 {
			note = "repo-local"
		}
		for _, pc := range sortedProjects(sr.projects) {
			// The skill name is repeated rather than blanked on continuation
			// rows, so any single line still says what it is about when it is
			// grepped or pasted out of context.
			skills = append(skills, sr.skill)
			projects = append(projects, prettyProject(pc.name))
			counts = append(counts, humanNum(pc.count))
			notes = append(notes, note)
		}
	}

	fmt.Fprintln(w)
	tbl := layout([]col{
		{head: "SKILL", cells: skills},
		{head: "PROJECT", flex: true, cells: projects},
		{head: "FIRINGS", right: true, cells: counts},
		{head: "NOTE", cells: notes, shed: 1},
	}, width, 2)
	fmt.Fprintln(w, paint(cDim, tbl[0]))
	for _, l := range tbl[1:] {
		fmt.Fprintln(w, l)
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
