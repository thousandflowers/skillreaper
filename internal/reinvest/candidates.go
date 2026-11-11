package reinvest

import (
	"sort"
	"strings"
)

const (
	candidatesPerGap = 20
	minInstalls      = 20 // skills.sh: fewer installs than this is too little signal to trust
	docFetchers      = 10
	manualOnlyReason = "runs only by hand: Claude could never start it"
)

// namespaced shows a registry title beside its namespace: the title is the
// server's own to choose, 'GitHub' from anyone.
func namespaced(m candidate) candidate {
	if m.Name == m.ID || !strings.Contains(m.ID, "/") {
		return m
	}
	owner, _, _ := strings.Cut(m.ID, "/")
	m.Name = m.Name + " (" + owner + ")"
	return m
}

// searchRank is how well a skill answers a gap's searches: how many of them
// found it, and its best place in their results.
type searchRank struct {
	hits int
	best int
}

func rankFor(c candidate, queries map[string]bool) searchRank {
	r := searchRank{}
	for q, place := range c.Hits {
		if !queries[q] {
			continue
		}
		if r.hits == 0 || place < r.best {
			r.best = place
		}
		r.hits++
	}
	return r
}

// rankedSkills orders the found skills for a gap by their best position in
// this gap's searches (installs reward off-topic skills).
func rankedSkills(gap kind, found []candidate) []candidate {
	queries := make(map[string]bool, len(gap.Queries))
	for _, q := range gap.Queries {
		queries[q] = true
	}
	var ranked []candidate
	ranks := map[string]searchRank{}
	for _, c := range found {
		if r := rankFor(c, queries); r.hits > 0 && c.Installs >= minInstalls {
			ranked = append(ranked, c)
			ranks[c.ID] = r
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranks[ranked[i].ID], ranks[ranked[j].ID]
		switch {
		case a.hits != b.hits:
			return a.hits > b.hits
		case a.best != b.best:
			return a.best < b.best
		}
		return ranked[i].Installs > ranked[j].Installs
	})
	return ranked[:min(candidatesPerGap, len(ranked))]
}

// candidatesFor gives a gap's candidates: skills first, then MCP servers
// unless noMCP. What the rules turn down comes back as refusals, so the
// report can say so.
func (a *app) candidatesFor(gap kind, found []candidate, noMCP bool) ([]candidate, []refusal) {
	ranked := rankedSkills(gap, found)
	docs := parallelMap(ranked, docFetchers, a.skillDoc)
	var out []candidate
	var refused []refusal
	for i, c := range ranked {
		doc := docs[i]
		desc := frontmatter(doc)["description"]
		if runeLen(desc) < minDesc {
			continue
		}
		if foundRisks := risks(doc, false); len(foundRisks) > 0 {
			refused = append(refused, refusal{c.ID, c.Name, securityPrefix + strings.Join(foundRisks, ", ")})
			continue
		}
		cost := a.reapTokens(c.Slug, doc)
		if cost == 0 {
			// Weighs nothing because Claude never lists it: it cannot take over any work, and any document can claim this.
			refused = append(refused, refusal{c.ID, c.Name, manualOnlyReason})
			continue
		}
		c.Doc, c.Desc, c.Cost = doc, desc, intPtr(cost)
		out = append(out, c)
	}
	if !noMCP {
		// cost nil: an MCP server's weight is its tool schemas, known only once it runs (reap reports it as unknown
		// too); running an unreviewed server to measure it is not acceptable before the user said yes
		for _, m := range a.searchMCP(gap.MCPQueries) {
			if foundRisks := risks(m.Doc, true); len(foundRisks) > 0 {
				refused = append(refused, refusal{m.ID, m.Name, securityPrefix + strings.Join(foundRisks, ", ")})
				continue
			}
			m = namespaced(m)
			m.Cost = nil
			out = append(out, m)
		}
	}
	for i := range out {
		out[i].Gap, out[i].Task = gap.gapLabel(), gap.Task
	}
	return out, refused
}
