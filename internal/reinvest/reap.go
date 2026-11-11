package reinvest

import (
	"fmt"
	"time"
)

const (
	reapTimeout  = 10 * time.Minute // reap reads every transcript: seconds on a warm cache, never this long
	verdictReap  = "REAP"
	categoryMCP  = "mcp"
	reapTailSize = 200
)

// reapFlags ask reap for its report as JSON, without the prompt it shows a person.
var reapFlags = []string{"--json", "--no-nudge"}

// reapState is what reap knows: installed skills, which ones fired, the REAP
// pool and MCP weights.
type reapState struct {
	Installed []skill
	Used      []skill
	Reap      []skill
	MCP       map[string]*int // by server name; nil when reap has not weighed it yet
	Dead      int             // dead tokens per session
}

func skillFromRow(row *obj) skill {
	s := skill{}
	s.Name, _ = row.str("Name")
	s.Desc, _ = row.str("Description")
	s.Tokens, _ = pyInt(row.val("Tokens"))
	s.Uses, _ = pyInt(row.val("Uses"))
	s.Verdict, _ = row.str("Verdict")
	return s
}

// parseReapState reads the output of `reap --json`.
func parseReapState(output string) (reapState, error) {
	v, err := parseJSON(output)
	if err != nil {
		return reapState{}, fmt.Errorf("`reap --json` did not answer with JSON: %w", err)
	}
	data, _ := v.(*obj)
	state := reapState{MCP: map[string]*int{}}
	state.Dead, _ = pyInt(data.val("DeadTokensPerSession"))
	for _, raw := range data.list("Rows") {
		row, _ := raw.(*obj)
		name, named := row.str("Name")
		if !named {
			continue
		}
		switch row.val("Category") {
		case kindSkill:
			s := skillFromRow(row)
			state.Installed = append(state.Installed, s)
			if s.Uses > 0 {
				state.Used = append(state.Used, s)
			}
			if s.Verdict == verdictReap {
				state.Reap = append(state.Reap, s)
			}
		case categoryMCP:
			state.MCP[name] = nil
			if !truthy(row.val("WeightUnknown")) {
				tokens, _ := pyInt(row.val("Tokens"))
				state.MCP[name] = intPtr(tokens)
			}
		}
	}
	return state, nil
}

// loadReapState asks the running reap for its report: weights and verdicts
// come from there, so the numbers match the reap report.
func (a *app) loadReapState() (reapState, error) {
	reap, err := a.reapCommand()
	if err != nil || len(reap) == 0 {
		return reapState{}, fmt.Errorf("reinvest could not find the reap binary to ask (set %s to its path): %v", reapVariable, err)
	}
	res := a.runProcess(procSpec{argv: append(append([]string{}, reap...), reapFlags...), timeout: reapTimeout})
	switch {
	case res.timedOut:
		return reapState{}, fmt.Errorf("`%s --json` gave no answer in %s", reap[0], reapTimeout)
	case res.err != nil:
		return reapState{}, fmt.Errorf("reinvest could not run reap (set %s to its path): %w", reapVariable, res.err)
	}
	state, err := parseReapState(res.stdout)
	if err != nil && res.code != 0 {
		return reapState{}, fmt.Errorf("`%s --json` exited %d: %s", reap[0], res.code, lastRunes(pyStrip(res.stderr), reapTailSize))
	}
	return state, err
}
