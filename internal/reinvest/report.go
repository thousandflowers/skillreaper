package reinvest

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	reportFile   = "report.md"
	verifiedFile = "verified.json"
	logFile      = "run.log"
	groupDigits  = 3
)

func (a *app) reportPath() string { return a.cachePath(reportFile) }

// thousands writes a number with commas between its thousands.
func thousands(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if n < 0 {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%groupDigits == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return sign + b.String()
}

// summary is the non-interactive view (no terminal): the same swaps, as text.
func (a *app) summary(swaps []swap, gaps []kind, dead int) string {
	lines := []string{"  reinvest · ~" + thousands(dead) + " dead tok/session per reap · swaps for work you do with no skill", ""}
	solved := map[string]bool{}
	for _, s := range swaps {
		c := s.cand()
		solved[c.Gap] = true
		lines = append(lines,
			"  mute      "+muteText(s),
			"  "+padRight(verb(c), verbWidth)+" "+c.Name+"   "+weight(c.Cost, c.Installed)+" · "+kindText(c),
			"            for "+c.Gap, "            "+c.Why, "")
	}
	var missing []string
	for _, g := range gaps {
		if !solved[g.gapLabel()] {
			missing = append(missing, g.Name)
		}
	}
	if len(missing) > 0 {
		lines = append(lines, "  no fit found for: "+strings.Join(missing, ", "), "")
	}
	return strings.Join(append(lines, "  details: "+a.reportPath()), "\n")
}

// gapName is a gap label without its count of sessions.
func gapName(label string) string {
	if i := strings.LastIndex(label, " ("); i >= 0 {
		return label[:i]
	}
	return label
}

func verifiedRecord(c candidate) *obj {
	var id, cost any
	if c.ID != "" {
		id = c.ID
	}
	if c.Cost != nil {
		cost = *c.Cost
	}
	return objOf("id", id, "name", c.Name, "gap", gapName(c.Gap), "fit", c.Fit, "why", c.Why, "cost", cost, "installs", c.Installs)
}

// writeReport leaves the details of the run in the cache: the kinds of work,
// the gaps, what passed and what was rejected, and why.
func (a *app) writeReport(kinds, gaps []kind, passing []candidate, rejected []rejection) error {
	lines := []string{"# reinvest", "", "## Kinds of work", ""}
	for _, k := range kinds {
		line := fmt.Sprintf("- %s: %d sessions, %d weeks", k.Name, k.Sessions, k.Weeks)
		if k.CoveredBy != "" {
			line += ", covered by " + k.CoveredBy
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "## Gaps", "")
	for _, g := range gaps {
		lines = append(lines, "- "+g.Name+": "+g.Task)
	}
	lines = append(lines, "", "## Verified candidates", "")
	records := make([]any, len(passing))
	for i, c := range passing {
		lines = append(lines, fmt.Sprintf("- %s (%s, %s, fit %d/5) for %s: %s", c.Name, kindText(c), weight(c.Cost, false), c.Fit, c.Gap, c.Why))
		records[i] = verifiedRecord(c)
	}
	lines = append(lines, "", "## Rejected", "")
	for _, r := range rejected {
		lines = append(lines, "- "+r.Name+": "+r.Reasons)
	}
	if err := writePrivate(a.reportPath(), strings.Join(lines, "\n")+"\n"); err != nil {
		return err
	}
	return writePrivate(a.cachePath(verifiedFile), dumpJSONText(records))
}

// refusalNotes is what the user is told about refusals: a failed check is not
// the same as nothing fitting.
func (a *app) refusalNotes(rejected []rejection) []string {
	var lines []string
	failed := 0
	for _, r := range rejected {
		if strings.Contains(r.Reasons, failedReason) {
			failed++
		}
		if strings.Contains(r.Reasons, securityPrefix) {
			lines = append(lines, "  refused for security: "+printable(r.Name)+": "+printable(r.Reasons))
		}
	}
	if failed > 0 {
		lines = append(lines, fmt.Sprintf("  the check could not run for %d · they count as not checked, not as no fit", failed))
	}
	if len(rejected) > 0 {
		lines = append(lines, "  details: "+a.reportPath())
	}
	return lines
}
