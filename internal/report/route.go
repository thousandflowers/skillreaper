package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// Usage-informed routing ("reap route") — the second stage after pruning.
//
// Pruning is subtractive: it removes dead weight. But a user with hundreds of
// legitimately-used skills still pays a resident-context cost that grows with
// library size. The category-router pattern trades that linear cost for a
// constant one by exposing only frequently-fired skills and pushing the long
// tail behind leaf routers loaded on demand.
//
// skillreaper's edge over a blind (BM25 / text-similarity) router is evidence:
// it organizes by what each skill *actually fired*, per project, from the same
// transcripts that drive its verdicts. Routing is strictly OPT-IN and SECONDARY
// to pruning, and the output is a PLAN — proposed, never auto-applied.

const (
	// RouteDefaultExposeThreshold: a surviving skill fired in at least this
	// fraction of sessions stays exposed (top-level); rarer ones get routed.
	RouteDefaultExposeThreshold = 0.10
	// RouteAdviceFloor: below this many surviving skills, native skill loading
	// is usually enough and routing mostly trades accuracy for tokens. The plan
	// still renders, but leads with that caveat.
	RouteAdviceFloor = 150
)

// Route tiers and reasons.
const (
	RouteTierExposed = "exposed"
	RouteTierRouted  = "routed"

	routeReasonFrequent   = "frequent"
	routeReasonRare       = "rare"
	routeReasonNoEvidence = "no-evidence"
)

// RouteSkill is one skill placed in the plan.
type RouteSkill struct {
	Name   string  `json:"name"`
	Tier   string  `json:"tier"`   // exposed | routed
	Reason string  `json:"reason"` // frequent | rare | no-evidence
	Uses   int     `json:"uses"`
	Tokens int     `json:"tokens"`
	Rate   float64 `json:"rate"` // uses/sessions; ≥0, can exceed 1 if fired many times per session
}

// RouteCategory is one leaf router holding rarely-fired skills, derived from
// evidence: a skill's namespace, else its dominant firing project, else misc.
type RouteCategory struct {
	Name      string       `json:"name"`
	Source    string       `json:"source"` // namespace | project | misc
	Skills    []RouteSkill `json:"skills"`
	RoutedTok int          `json:"routed_tok"`
}

// RoutePlan is the proposed lazy-load organization for surviving skills.
type RoutePlan struct {
	TotalSkills     int             `json:"total_skills"`
	Sessions        int             `json:"sessions"`
	ExposeThreshold float64         `json:"expose_threshold"`
	BelowFloor      bool            `json:"below_floor"`
	Exposed         []RouteSkill    `json:"exposed"`
	Categories      []RouteCategory `json:"categories"`
	ExposedTok      int             `json:"exposed_tok"` // stays resident every session
	RoutedTok       int             `json:"routed_tok"`  // moved behind routers (the win)
	// Skipped marks that the caller's --route-min-skills gate suppressed the
	// plan (too few surviving skills to bother routing). Rendered in every
	// format so JSON/Markdown/text stay in parity.
	Skipped   bool `json:"skipped"`
	MinSkills int  `json:"min_skills,omitempty"`
}

// BuildRoutePlan classifies every skill that would survive a prune into exposed
// (top-level) or routed (behind a category leaf) tiers, using firing evidence.
// REAP skills are excluded — they belong to `reap prune`, not routing. A
// non-positive exposeThreshold falls back to the default.
func BuildRoutePlan(r *Report, exposeThreshold float64) *RoutePlan {
	if exposeThreshold <= 0 {
		exposeThreshold = RouteDefaultExposeThreshold
	}
	plan := &RoutePlan{Sessions: r.Sessions, ExposeThreshold: exposeThreshold}
	cats := map[string]*RouteCategory{}

	for _, row := range r.Rows {
		if row.Category != scan.CatSkill || row.Verdict == VerdictReap {
			continue
		}
		plan.TotalSkills++

		rate := 0.0
		if r.Sessions > 0 {
			rate = float64(row.Uses) / float64(r.Sessions)
		}
		rs := RouteSkill{Name: row.Name, Uses: row.Uses, Tokens: row.Tokens, Rate: rate}

		switch {
		case r.Sessions == 0 || row.Uses == 0:
			// No firing evidence — routing blind would be guesswork, so keep it
			// exposed. This preserves skillreaper's "never act on absent evidence".
			rs.Tier, rs.Reason = RouteTierExposed, routeReasonNoEvidence
			plan.Exposed = append(plan.Exposed, rs)
			plan.ExposedTok += rs.Tokens
		case rate >= exposeThreshold:
			rs.Tier, rs.Reason = RouteTierExposed, routeReasonFrequent
			plan.Exposed = append(plan.Exposed, rs)
			plan.ExposedTok += rs.Tokens
		default:
			rs.Tier, rs.Reason = RouteTierRouted, routeReasonRare
			name, src := routeCategory(r, row.Name)
			key := src + ":" + name
			c := cats[key]
			if c == nil {
				c = &RouteCategory{Name: name, Source: src}
				cats[key] = c
			}
			c.Skills = append(c.Skills, rs)
			c.RoutedTok += rs.Tokens
			plan.RoutedTok += rs.Tokens
		}
	}

	plan.Exposed = sortRouteSkills(plan.Exposed)
	plan.Categories = sortCategories(cats)
	plan.BelowFloor = plan.TotalSkills < RouteAdviceFloor
	return plan
}

// routeCategory derives a leaf-router name for a rarely-fired skill from
// evidence: its namespace prefix ("ecc:plan" → "ecc"), else its dominant firing
// project, else "misc".
func routeCategory(r *Report, name string) (catName, source string) {
	if i := strings.IndexByte(name, ':'); i > 0 {
		return name[:i], "namespace"
	}
	if projs := skillProjects(r, name); len(projs) > 0 {
		top := sortedProjects(projs)
		if len(top) > 0 {
			return prettyProject(top[0].name), "project"
		}
	}
	return "misc", "misc"
}

// skillProjects returns a skill's project-firing buckets. It is only reached for
// non-namespaced names (routeCategory handles namespaced skills before calling),
// so a plain name lookup is sufficient.
func skillProjects(r *Report, name string) map[string]int {
	return r.SkillProjects[name]
}

// sortRouteSkills orders skills by firing count desc, then tokens desc, then name.
func sortRouteSkills(s []RouteSkill) []RouteSkill {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Uses != s[j].Uses {
			return s[i].Uses > s[j].Uses
		}
		if s[i].Tokens != s[j].Tokens {
			return s[i].Tokens > s[j].Tokens
		}
		return s[i].Name < s[j].Name
	})
	return s
}

// sortCategories materializes the category map into a slice ordered by deferred
// tokens desc, then name — heaviest leaf routers (biggest savings) first.
func sortCategories(cats map[string]*RouteCategory) []RouteCategory {
	out := make([]RouteCategory, 0, len(cats))
	for _, c := range cats {
		c.Skills = sortRouteSkills(c.Skills)
		out = append(out, *c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RoutedTok != out[j].RoutedTok {
			return out[i].RoutedTok > out[j].RoutedTok
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		// Same name, different source (namespace vs project vs misc) — break the
		// tie deterministically so the plan is stable/diffable.
		return out[i].Source < out[j].Source
	})
	return out
}

// RenderRoutePlan writes the human-readable routing plan.
func RenderRoutePlan(w io.Writer, plan *RoutePlan, color bool) {
	paint := painter(color)
	if plan.Skipped {
		fmt.Fprintf(w, "\n%s\n  %s\n", paint(cBold, "reap route"),
			paint(cDim, fmt.Sprintf("Only %d skills survive a prune (below --route-min-skills=%d). Routing isn't worth it yet — prune first.", plan.TotalSkills, plan.MinSkills)))
		return
	}
	fmt.Fprintf(w, "\n%s\n", paint(cBold, "reap route — usage-informed lazy-load plan (proposed, never applied)"))
	fmt.Fprintf(w, "  %s\n\n",
		paint(cDim, fmt.Sprintf("%d skills survive a prune · %d sessions · expose threshold %.0f%%",
			plan.TotalSkills, plan.Sessions, plan.ExposeThreshold*100)))

	if plan.BelowFloor {
		fmt.Fprintf(w, "  %s\n", paint(cBYell, fmt.Sprintf("ⓘ %d skills is below ~%d — native skill loading is usually enough here.", plan.TotalSkills, RouteAdviceFloor)))
		fmt.Fprintf(w, "  %s\n\n", paint(cDim, "Routing trades a little selection accuracy for token savings. Prune first; route only if context is tight."))
	}

	fmt.Fprintf(w, "  %s  %s\n", paint(cBold, "EXPOSED"), paint(cDim, fmt.Sprintf("(stay top-level)            ~%s tok resident", humanChars(plan.ExposedTok))))
	if len(plan.Exposed) == 0 {
		fmt.Fprintf(w, "    %s\n", paint(cDim, "(none)"))
	}
	for _, s := range plan.Exposed {
		fmt.Fprintf(w, "    %-40s %4d×  %s\n", truncate(s.Name, 40), s.Uses, paint(cDim, ratePct(s.Rate)+"  "+s.Reason))
	}

	fmt.Fprintf(w, "\n  %s  %s\n", paint(cBold, "ROUTED"), paint(cDim, fmt.Sprintf("(loaded on demand)         ~%s tok deferred", humanChars(plan.RoutedTok))))
	if len(plan.Categories) == 0 {
		fmt.Fprintf(w, "    %s\n", paint(cDim, "(none — every surviving skill fires often enough to stay exposed)"))
	}
	for _, c := range plan.Categories {
		fmt.Fprintf(w, "    %s %s\n", paint(cBYell, "▸ "+routeCatLabel(c)), paint(cDim, fmt.Sprintf("~%s tok", humanChars(c.RoutedTok))))
		for _, s := range c.Skills {
			fmt.Fprintf(w, "        %-36s %4d×  %s\n", truncate(s.Name, 36), s.Uses, paint(cDim, ratePct(s.Rate)))
		}
	}

	fmt.Fprintf(w, "\n  %s\n", paint(cBold, fmt.Sprintf("Net: ~%s tok/session moved out of resident context.", humanChars(plan.RoutedTok))))
	fmt.Fprintf(w, "  %s\n", paint(cDim, "This is a plan — nothing is applied. Pair with `reap mute` to also strip the descriptions of routed skills."))
}

// routeCatLabel formats a category header like "ecc (namespace)".
func routeCatLabel(c RouteCategory) string {
	return fmt.Sprintf("%s (%s)", c.Name, c.Source)
}

// ratePct renders a 0..1 rate as a whole percent.
func ratePct(rate float64) string {
	return fmt.Sprintf("%d%%", int(rate*100+0.5))
}

// RenderRoutePlanJSON writes the plan as indented JSON.
func RenderRoutePlanJSON(w io.Writer, plan *RoutePlan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(plan)
}

// RenderRoutePlanMarkdown writes the plan as Markdown.
func RenderRoutePlanMarkdown(w io.Writer, plan *RoutePlan) {
	if plan.Skipped {
		fmt.Fprintf(w, "# reap route\n\n_Only %d skills survive a prune (below --route-min-skills=%d). Routing isn't worth it yet — prune first._\n", plan.TotalSkills, plan.MinSkills)
		return
	}
	fmt.Fprintf(w, "# reap route — usage-informed lazy-load plan\n\n")
	fmt.Fprintf(w, "_Proposed, never applied. %d skills survive a prune · %d sessions · expose threshold %.0f%%._\n\n",
		plan.TotalSkills, plan.Sessions, plan.ExposeThreshold*100)
	if plan.BelowFloor {
		fmt.Fprintf(w, "> ⓘ %d skills is below ~%d — native skill loading is usually enough. Prune first; route only if context is tight.\n\n", plan.TotalSkills, RouteAdviceFloor)
	}

	fmt.Fprintf(w, "## Exposed (stay top-level) — ~%s tok resident\n\n", humanChars(plan.ExposedTok))
	fmt.Fprintln(w, "| Skill | Uses | Rate | Reason |")
	fmt.Fprintln(w, "|---|---|---|---|")
	for _, s := range plan.Exposed {
		fmt.Fprintf(w, "| %s | %d | %s | %s |\n", s.Name, s.Uses, ratePct(s.Rate), s.Reason)
	}

	fmt.Fprintf(w, "\n## Routed (loaded on demand) — ~%s tok deferred\n\n", humanChars(plan.RoutedTok))
	for _, c := range plan.Categories {
		fmt.Fprintf(w, "### ▸ %s — ~%s tok\n\n", routeCatLabel(c), humanChars(c.RoutedTok))
		fmt.Fprintln(w, "| Skill | Uses | Rate |")
		fmt.Fprintln(w, "|---|---|---|")
		for _, s := range c.Skills {
			fmt.Fprintf(w, "| %s | %d | %s |\n", s.Name, s.Uses, ratePct(s.Rate))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "**Net: ~%s tok/session moved out of resident context.** Pair with `reap mute` to strip routed skills' descriptions.\n", humanChars(plan.RoutedTok))
}
