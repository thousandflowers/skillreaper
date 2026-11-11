package reinvest

import "sort"

// skill is one installed skill as reap reports it.
type skill struct {
	Name    string
	Desc    string
	Tokens  int
	Uses    int
	Verdict string
}

// session is one conversation a person had with Claude Code.
type session struct {
	ID         string
	Prompts    []string
	Tools      int
	Errors     int
	Interrupts int
	Tokens     int
	Skills     map[string]bool
	TS         string
}

func (s session) skillList() []string {
	out := make([]string, 0, len(s.Skills))
	for name := range s.Skills {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// kind is one kind of work, with what the sessions doing it add up to.
type kind struct {
	Name         string
	Task         string
	Delegable    bool
	Queries      []string
	MCPQueries   []string
	IDs          []string
	Sessions     int
	Weeks        int
	Tokens       int
	MedianTokens int
	Friction     int
	CoveredBy    string // a skill used in most of these sessions, or ""
	Examples     []string
}

// gapLabel is how a gap is named beside a candidate for it.
func (k kind) gapLabel() string { return gapLabel(k.Name, k.Sessions) }

const (
	kindSkill = "skill"
	kindMCP   = "mcp"
)

// candidate is something that could do a kind of work: a skill from
// skills.sh, an MCP server from the registry, or a skill written here.
type candidate struct {
	ID        string
	Kind      string
	Source    string // owner/repo, for a skill from skills.sh
	Slug      string // the skill's folder
	Name      string
	Installs  int
	Hits      map[string]int // search query -> rank in its results
	Doc       string         // everything installing it brings, as the verifier reads it
	Desc      string
	Cost      *int // tokens per session; nil when unknown until installed
	Gap       string
	Task      string
	Server    *obj // the registry's entry, for an MCP server
	Created   bool // written from the person's own sessions
	Installed bool // an MCP server an earlier run installed
	Where     string
	Fit       int
	Why       string
}

func intPtr(n int) *int { return &n }

// costOrZero is `cost or 0`.
func (c candidate) costOrZero() int {
	if c.Cost == nil {
		return 0
	}
	return *c.Cost
}

// swap is one proposal: mute these, take that candidate.
type swap struct {
	Mute  []skill
	Out   int // tokens the muted skills free
	Cands []candidate
	Idx   int
	OK    bool
	Held  []skill // what `m` set aside, to mute again on the next `m`
}

func (s swap) cand() candidate { return s.Cands[s.Idx] }

// verdict is what one check of one candidate concluded.
type verdict struct {
	Keep   bool
	Risk   string
	Fit    int
	Reason string
	Use    string
	At     float64 // when it was reached, for the cache
}

// refusal is a candidate turned down, and why.
type refusal struct {
	ID     string
	Name   string
	Reason string
}

// rejection is one line of the report: a candidate and every reason against it.
type rejection struct {
	Name    string
	Reasons string
}

func sumTokens(skills []skill) int {
	total := 0
	for _, s := range skills {
		total += s.Tokens
	}
	return total
}

func containsSkill(list []skill, s skill) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// withoutSkills is [x for x in pool if x not in drop].
func withoutSkills(pool, drop []skill) []skill {
	out := make([]skill, 0, len(pool))
	for _, x := range pool {
		if !containsSkill(drop, x) {
			out = append(out, x)
		}
	}
	return out
}
