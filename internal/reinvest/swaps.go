package reinvest

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Swaps: mute skills you never use, install one that does work you actually
// do, for about the same tokens. Muting goes through `reap mute` (reversible
// with `reap unmute`), so it works for plugin skills too.

const (
	maxMutedPerSwap = 3
	pairCeiling     = 1.3 // a pair may weigh this much of the candidate, no more
	pairFloor       = 0.7 // and the best match must free at least this much of it
	// MCP servers installed with nothing muted, because their weight was unknown. Once reap has seen one load, the
	// next run offers the mute that should have gone with it.
	weighLaterFile = "weigh_later.json"
	// The last proposals, for the one-line pointer in reap's text report. It only points: applying stays in the picker.
	swapsFile    = "swaps.json"
	rerunCommand = "reap reinvest"
	namesShown   = 3
	defaultFit   = 1
)

func distance(skills []skill, cost int) int {
	d := sumTokens(skills) - cost
	if d < 0 {
		return -d
	}
	return d
}

// pair picks the REAP skills whose weight adds up to about cost: the closest
// single or pair, else the heaviest few. Nothing for an unknown weight:
// muting against a guess is how a 2800-token estimate muted 706 real tokens.
func pair(cost *int, pool []skill) []skill {
	if cost == nil {
		return nil
	}
	want := *cost
	var best []skill
	better := func(option []skill) bool {
		return best == nil || distance(option, want) < distance(best, want) ||
			(distance(option, want) == distance(best, want) && len(option) < len(best))
	}
	for _, x := range pool {
		if option := []skill{x}; better(option) {
			best = option
		}
	}
	for i, a := range pool {
		for _, b := range pool[i+1:] {
			if option := []skill{a, b}; float64(a.Tokens+b.Tokens) <= float64(want)*pairCeiling && better(option) {
				best = option
			}
		}
	}
	if float64(sumTokens(best)) >= float64(want)*pairFloor {
		return best
	}
	heaviest := append([]skill{}, pool...)
	sort.SliceStable(heaviest, func(i, j int) bool { return heaviest[i].Tokens > heaviest[j].Tokens })
	heaviest = heaviest[:min(maxMutedPerSwap, len(heaviest))]
	var out []skill
	for i, x := range heaviest {
		if sumTokens(heaviest[:i]) < want {
			out = append(out, x)
		}
	}
	return out
}

func skillNames(skills []skill) []any {
	out := make([]any, len(skills))
	for i, s := range skills {
		out[i] = s.Name
	}
	return out
}

func proposal(s swap) *obj {
	return objOf("install", s.cand().Name, "mute", skillNames(s.Mute))
}

// saveProposals leaves the proposals where reap's report looks for them.
func (a *app) saveProposals(swaps []swap) error {
	list := make([]any, len(swaps))
	for i, s := range swaps {
		list[i] = proposal(s)
	}
	record := objOf("at", a.now().UTC().Format("2006-01-02T15:04:05Z"), "command", rerunCommand, "swaps", list)
	return writeShared(a.cachePath(swapsFile), dumpJSONText(record))
}

// dropProposals forgets the swaps the user acted on: they no longer need pointing at.
func (a *app) dropProposals(applied []swap) error {
	data, ok := readJSONFile(a.cachePath(swapsFile)).(*obj)
	if !ok {
		return nil
	}
	var left []any
	for _, x := range data.list("swaps") {
		done := false
		for _, s := range applied {
			done = done || jsonEqual(x, proposal(s))
		}
		if !done {
			left = append(left, x)
		}
	}
	if left == nil {
		left = []any{}
	}
	return writeShared(a.cachePath(swapsFile), dumpJSONText(data.with("swaps", left)))
}

// build makes one swap per pick; its candidates are every verified
// candidate, same gap first, closest weight next.
func build(picks, passing []candidate, reapPool []skill) []swap {
	pool := append([]skill{}, reapPool...)
	var swaps []swap
	for _, pick := range picks {
		mute := pair(pick.Cost, pool)
		if len(mute) == 0 && pick.Cost != nil && !pick.Created {
			continue // a written skill is worth offering on its own: nothing to mute is fine
		}
		pool = withoutSkills(pool, mute)
		out := sumTokens(mute)
		cands := append([]candidate{}, passing...)
		sort.SliceStable(cands, func(i, j int) bool { return closer(cands[i], cands[j], pick.Gap, out) })
		idx := 0
		for i, c := range cands {
			if c.Name == pick.Name {
				idx = i
				break
			}
		}
		swaps = append(swaps, swap{Mute: mute, Out: out, Cands: cands, Idx: idx})
	}
	return swaps
}

// closer orders a swap's candidates: the pick's gap first, then the better
// fit, then a known weight, then the weight nearest what is muted.
func closer(a, b candidate, gap string, out int) bool {
	switch {
	case (a.Gap != gap) != (b.Gap != gap):
		return a.Gap == gap
	case a.Fit != b.Fit:
		return a.Fit > b.Fit
	case (a.Cost == nil) != (b.Cost == nil):
		return b.Cost == nil
	}
	return math.Abs(float64(a.costOrZero()-out)) < math.Abs(float64(b.costOrZero()-out))
}

func weight(cost *int, measured bool) string {
	if cost == nil {
		return "weight unknown until installed"
	}
	text := "~" + strconv.Itoa(*cost) + " tok/session"
	if measured {
		text += " · measured"
	}
	return text
}

func verb(c candidate) string {
	switch {
	case c.Installed:
		return "installed"
	case c.Created:
		return "create"
	}
	return "install"
}

func kindText(c candidate) string {
	switch {
	case c.Created:
		return "✎ new · written from your sessions"
	case c.Kind == kindMCP && c.Where != "":
		return "MCP server, " + c.Where
	case c.Kind == kindMCP:
		return "MCP server"
	case c.Source != "":
		return c.Kind + " · " + c.Source
	}
	return c.Kind
}

// named is the name, with the repo it really comes from: a registry name is
// free text and can claim to be anyone.
func named(c candidate) string {
	if c.Source != "" {
		return c.Name + " (" + c.Source + ")"
	}
	return c.Name
}

// toggleMute mutes nothing for this swap, or the same skills again: the held
// ones stay out of every other swap.
func toggleMute(s swap) swap {
	s.Mute, s.Held = s.Held, s.Mute
	s.Out = sumTokens(s.Mute)
	return s
}

func muteText(s swap) string {
	switch {
	case len(s.Mute) == 0 && len(s.Held) > 0:
		return "nothing · m to mute " + names(s.Held) + " again"
	case len(s.Mute) == 0 && s.cand().Cost != nil:
		return "nothing · no unused skill of about its weight"
	case len(s.Mute) == 0:
		return "nothing · the weight of what it installs is unknown, so nothing is traded for it"
	}
	return fmt.Sprintf("%s   ~%d tok/session · never used", names(s.Mute), s.Out)
}

func names(mute []skill) string {
	if len(mute) == 0 {
		return "nothing"
	}
	shown := make([]string, 0, namesShown)
	for _, x := range mute[:min(namesShown, len(mute))] {
		shown = append(shown, x.Name)
	}
	text := strings.Join(shown, " + ")
	if len(mute) > namesShown {
		text += fmt.Sprintf(" + %d more", len(mute)-namesShown)
	}
	return text
}

// ---------------------------------------------------------------------------
// MCP servers weighed after install
// ---------------------------------------------------------------------------

func mcpName(c candidate) string {
	if name, ok := c.Server.str("name"); ok {
		return lastSegment(name)
	}
	return lastSegment(c.Name)
}

func (a *app) waiting() *obj {
	if saved, ok := readJSONFile(a.cachePath(weighLaterFile)).(*obj); ok {
		return saved
	}
	return newObj()
}

func (a *app) saveWaiting(waiting *obj) error {
	return writeShared(a.cachePath(weighLaterFile), dumpJSONText(waiting))
}

// rememberServer notes an MCP server installed with nothing muted.
func (a *app) rememberServer(c candidate) error {
	fit := c.Fit
	if fit == 0 {
		fit = defaultFit
	}
	return a.saveWaiting(a.waiting().with(mcpName(c), objOf("name", c.Name, "gap", c.Gap, "why", c.Why, "fit", fit)))
}

func (a *app) forgetServer(c candidate) error {
	return a.saveWaiting(a.waiting().without(mcpName(c)))
}

// settle gives the swaps for MCP servers installed earlier with nothing
// muted, now that reap has weighed them. weights is reap's MCP rows by name
// (nil: not weighed yet). A server reap no longer lists was removed: it is
// forgotten. One not weighed yet waits for a later run.
func (a *app) settle(weights map[string]*int, reapPool []skill) ([]swap, error) {
	saved := a.waiting()
	waiting := newObj()
	for _, key := range saved.keyList() {
		if _, listed := weights[key]; listed {
			waiting.set(key, saved.val(key))
		}
	}
	if err := a.saveWaiting(waiting); err != nil {
		return nil, err
	}
	pool := append([]skill{}, reapPool...)
	var out []swap
	for _, key := range waiting.keyList() {
		entry, ok := waiting.val(key).(*obj)
		if !ok {
			continue
		}
		cost := weights[key]
		mute := pair(cost, pool)
		if len(mute) == 0 {
			continue
		}
		pool = withoutSkills(pool, mute)
		c := candidate{Kind: kindMCP, Cost: cost, Installed: true, Server: objOf("name", key)}
		c.Name, _ = entry.str("name")
		c.Gap, _ = entry.str("gap")
		c.Why, _ = entry.str("why")
		c.Fit, _ = pyInt(entry.val("fit"))
		out = append(out, swap{Mute: mute, Out: sumTokens(mute), Cands: []candidate{c}})
	}
	return out, nil
}
