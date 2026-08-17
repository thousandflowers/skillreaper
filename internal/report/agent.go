package report

import (
	"fmt"
	"io"
	"sort"
)

// AgentMaxRows caps the dead-item table in the agent format. The point of this
// format is to spend as little of an agent's context as possible, so a full
// inventory would defeat it — anyone who needs every row uses --json.
const AgentMaxRows = 10

// agentSignature closes every agent report. Unlike RenderFooter this is not
// reached through a branch of cmdReport: it is written unconditionally here,
// with no colour, no cooldown and no NudgeState, because the whole purpose of
// this format is a SKILL.md with zero rendering rules. If the signature were
// the skill's job, one formatting instruction would survive in the prompt —
// and that is the crack a prose spec grows back through.
const agentSignature = "measured by skillreaper · github.com/thousandflowers/skillreaper"

// pctOf renders a percentage for a ratio that may legitimately have no
// denominator. A zero denominator is not an error: MCP items carry their weight
// in tool schemas rather than in an injected description, so their token counts
// are genuinely 0 and must not be reported as 0%.
func pctOf(part, whole int) string {
	if whole == 0 {
		return "n/a"
	}
	p := part * 100 / whole
	if p == 0 && part > 0 {
		return "<1%"
	}
	return fmt.Sprintf("%d%%", p)
}

// RenderAgent writes the report as compact plain text meant to be pasted
// verbatim by an agent: no ANSI, no bars, no box drawing, no terminal-width
// padding. Deterministic for a given Report — the same input always yields the
// same bytes, which is what makes it testable in a way a prose render spec
// never was. GeneratedAt is deliberately omitted for the same reason.
func RenderAgent(w io.Writer, r *Report) {
	fmt.Fprintf(w, "skillreaper · last %dd · %d sessions\n", r.WindowDays, r.Sessions)

	if r.Gap != nil && r.Gap.Loaded > 0 {
		fmt.Fprintf(w, "%d/%d items fired · %s utilization\n",
			r.Gap.Fired, r.Gap.Loaded, pctOf(r.Gap.Fired, r.Gap.Loaded))
	} else {
		fmt.Fprintln(w, "no items loaded")
	}

	// No money line here, unlike every other renderer. A dollar figure is a
	// token count multiplied by a price we do not control and that moves when a
	// provider updates its list; a format whose whole contract is "paste this
	// verbatim" must not carry the one number that ages badly. The item count
	// and the token count are measured facts and survive a price change.
	fmt.Fprintf(w, "%d never used · ~%d dead tokens/session\n",
		r.DeadCount, r.DeadTokensPerSession)

	dead := deadRows(r)
	if len(dead) == 0 {
		fmt.Fprintln(w, "\nNothing unused in this window.")
	} else {
		shown := dead
		if len(shown) > AgentMaxRows {
			shown = shown[:AgentMaxRows]
		}
		fmt.Fprintln(w)
		tw := newTable(w)
		tw.row("TOKENS", "CATEGORY", "NAME", "VERDICT", "REASON")
		for _, row := range shown {
			// Names are never truncated: a clipped identifier is not something
			// the reader can act on, and the column is sized to the content.
			tw.row(fmt.Sprintf("%d", row.Tokens), string(row.Category), row.Name, row.Verdict, row.Reason)
		}
		tw.flush()
		if len(dead) > AgentMaxRows {
			fmt.Fprintf(w, "(%d more never-used items not shown — use --json for all)\n", len(dead)-AgentMaxRows)
		}
		fmt.Fprintln(w, "\nTo prune: reap prune   (interactive, reversible via reap restore --all)")
	}

	if len(r.Warnings) > 0 {
		fmt.Fprintf(w, "\n%d warnings — some evidence was incomplete; those items were held back from a REAP verdict.\n", len(r.Warnings))
	}

	fmt.Fprintf(w, "\n%s\n", agentSignature)
}

// deadRows returns the REAP rows heaviest first. Ties break on name so the
// order never depends on scan order.
func deadRows(r *Report) []Row {
	var dead []Row
	for _, row := range r.Rows {
		if row.Verdict == VerdictReap {
			dead = append(dead, row)
		}
	}
	sort.SliceStable(dead, func(i, j int) bool {
		if dead[i].Tokens != dead[j].Tokens {
			return dead[i].Tokens > dead[j].Tokens
		}
		return dead[i].Name < dead[j].Name
	})
	return dead
}

// RenderGapAgent writes the loaded-versus-fired breakdown in the same
// paste-verbatim form as RenderAgent.
func RenderGapAgent(w io.Writer, r *Report) {
	fmt.Fprintln(w, "skillreaper · loaded vs fired")

	if r.Gap == nil || r.Gap.Loaded == 0 {
		fmt.Fprintln(w, "\nno items loaded")
		fmt.Fprintf(w, "\n%s\n", agentSignature)
		return
	}

	g := r.Gap
	fmt.Fprintf(w, "%d/%d items fired · %s utilization\n", g.Fired, g.Loaded, pctOf(g.Fired, g.Loaded))
	fmt.Fprintf(w, "%d/%d tokens touched · %s token reach\n", g.FiredTok, g.LoadedTok, pctOf(g.FiredTok, g.LoadedTok))

	fmt.Fprintln(w)
	tw := newTable(w)
	tw.row("CATEGORY", "LOADED", "FIRED", "UTIL", "LOADED TOK", "TOUCHED TOK", "REACH")
	zeroWeight := false
	for _, c := range g.PerCat {
		if c.LoadedTok == 0 {
			zeroWeight = true
		}
		tw.row(
			string(c.Category),
			fmt.Sprintf("%d", c.Loaded),
			fmt.Sprintf("%d", c.Fired),
			pctOf(c.Fired, c.Loaded),
			fmt.Sprintf("%d", c.LoadedTok),
			fmt.Sprintf("%d", c.FiredTok),
			pctOf(c.FiredTok, c.LoadedTok),
		)
	}
	tw.flush()

	if zeroWeight {
		fmt.Fprintln(w, "\nREACH n/a means there are no loaded tokens to divide by: that weight lives")
		fmt.Fprintln(w, "in tool schemas rather than in an injected description, so it is not counted.")
	}

	if noisy := noisyPayload(r); len(noisy) > 0 {
		fmt.Fprintln(w, "\nNOISY TOOLS (fires often, mostly returns noise)")
		nt := newTable(w)
		nt.row("TOOL", "CALLS", "SIGNAL")
		for _, p := range noisy {
			nt.row(p.Tool, fmt.Sprintf("%d", p.Calls), fmt.Sprintf("%d%%", p.QualityPct))
		}
		nt.flush()
	} else if len(r.MCPPayload) > 0 {
		fmt.Fprintf(w, "\n%d tools measured, none flagged noisy.\n", len(r.MCPPayload))
	}

	fmt.Fprintf(w, "\n%s\n", agentSignature)
}

// noisyPayload returns the flagged payload rows in the order the report
// carries them, which computePayload has already made deterministic.
func noisyPayload(r *Report) []PayloadRow {
	var out []PayloadRow
	for _, p := range r.MCPPayload {
		if p.Noisy {
			out = append(out, p)
		}
	}
	return out
}
