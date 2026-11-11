package reinvest

import (
	"path/filepath"
	"sort"
	"strconv"
)

const (
	regroupEvery      = 7 * day // a full regroup changes the groups (0-18% overlap run to run), so it runs weekly; in between, only new sessions are placed into the groups that exist
	memorySessions    = 1000    // about 1 MB at most: what the file keeps of each session is capped below
	memoryPrompts     = 10
	memoryPromptChars = 500
	minSessions       = 3 // a kind of work seen fewer times is not recurring
	maxGaps           = 6
	memoryFile        = "sessions_memory.json"
	groupsFile        = "groups.json"
	exampleChars      = 200
	examplesPerKind   = 6
	interruptWeight   = 3
	coveredShare      = 0.5 // a skill used in at least this share of a kind's sessions covers it
	tokenScale        = 200_000
)

func gapLabel(name string, sessions int) string {
	return name + " (" + strconv.Itoa(sessions) + " sessions)"
}

func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func countOr(o *obj, key string) int {
	n, _ := pyInt(o.val(key))
	return n
}

// rememberedSession reads one record of the memory file. ok is false for
// anything that is not a session: a damaged entry is dropped, not a crash.
func rememberedSession(v any) (session, bool) {
	r, ok := v.(*obj)
	if !ok {
		return session{}, false
	}
	id, okID := r.str("id")
	rawPrompts, okPrompts := r.val("prompts").([]any)
	prompts := stringList(r.val("prompts"))
	if !okID || !okPrompts || len(prompts) != len(rawPrompts) {
		return session{}, false
	}
	s := session{ID: id, Prompts: prompts, Tools: countOr(r, "tools"), Errors: countOr(r, "errors"),
		Interrupts: countOr(r, "interrupts"), Tokens: countOr(r, "tokens"), Skills: map[string]bool{}}
	s.TS, _ = r.str("ts")
	for _, name := range stringList(r.val("skills")) {
		s.Skills[name] = true
	}
	return s, true
}

func sessionRecord(s session) *obj {
	prompts := make([]any, 0, memoryPrompts)
	for _, p := range s.Prompts[:min(memoryPrompts, len(s.Prompts))] {
		prompts = append(prompts, firstRunes(p, memoryPromptChars))
	}
	skills := make([]any, 0, len(s.Skills))
	for _, name := range s.skillList() {
		skills = append(skills, name)
	}
	return objOf("id", s.ID, "prompts", prompts, "tools", s.Tools, "errors", s.Errors, "interrupts", s.Interrupts,
		"tokens", s.Tokens, "skills", skills, "ts", s.TS)
}

// rememberSessions gives the sessions on disk plus those already remembered:
// Claude Code deletes old transcripts, the work they show stays.
func (a *app) rememberSessions(current []session) ([]session, error) {
	path := filepath.Join(a.stateDir, memoryFile)
	known := make(map[string]bool, len(current))
	for _, s := range current {
		known[s.ID] = true
	}
	merged := append([]session{}, current...)
	saved, _ := readJSONFile(path).([]any)
	old := map[string]int{} // a remembered id met twice keeps its first place and its last record
	for _, raw := range saved {
		s, ok := rememberedSession(raw)
		if !ok || known[s.ID] {
			continue
		}
		if at, seen := old[s.ID]; seen {
			merged[at] = s
			continue
		}
		old[s.ID] = len(merged)
		merged = append(merged, s)
	}
	newest := append([]session{}, merged...)
	sort.SliceStable(newest, func(i, j int) bool { return newest[i].TS > newest[j].TS })
	records := make([]any, 0, a.memorySessions)
	for _, s := range newest[:min(a.memorySessions, len(newest))] {
		records = append(records, sessionRecord(s))
	}
	if err := writePrivate(path, dumpJSONText(records)); err != nil {
		return nil, err
	}
	return merged, nil
}

func idSet(sessions []session) map[string]bool {
	out := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		out[s.ID] = true
	}
	return out
}

func sortedKeys(set map[string]bool) []any {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = k
	}
	return out
}

func unixSeconds(a *app) float64 { return float64(a.now().UnixNano()) / 1e9 }

// number reads a JSON number as a float.
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// savedGroups is groups.json when it is the current format: the groups, when
// they were made and which sessions they have seen.
type savedGroups struct {
	at     any // the number as it was written
	groups []*obj
	seen   map[string]bool
}

func (a *app) loadSavedGroups() (savedGroups, bool) {
	saved, ok := readJSONFile(a.cachePath(groupsFile)).(*obj)
	if !ok {
		return savedGroups{}, false
	}
	out := savedGroups{at: saved.val("at"), seen: map[string]bool{}}
	rawGroups, hasGroups := saved.val("groups").([]any)
	for _, raw := range rawGroups {
		if g, isObject := raw.(*obj); isObject {
			out.groups = append(out.groups, g)
		}
	}
	for _, id := range stringList(saved.val("seen")) {
		out.seen[id] = true
	}
	return out, hasGroups
}

// isDue reports whether the groups are a week old. A date that is missing or
// not a number makes them due.
func (a *app) isDue(saved savedGroups) bool {
	at, isNumber := number(saved.at)
	return !isNumber || unixSeconds(a)-at >= regroupEvery.Seconds()
}

// loadGroups gives the kinds of work: grouped from scratch when the cache is
// a week old (or on request), else the cached groups with the new sessions
// placed in them.
func (a *app) loadGroups(sessions []session, regroup bool) ([]*obj, error) {
	saved, usable := a.loadSavedGroups()
	var groups []*obj
	var at any
	seen := saved.seen
	if regroup || !usable || a.isDue(saved) {
		fresh, answered, err := a.groupAll(sessions)
		if err != nil {
			return nil, err
		}
		if !answered { // a call that never answered is not an answer: nothing saved, the next run asks again
			a.failed("grouping your work got no answer · trying again next run")
			return saved.groups, nil
		}
		groups, at, seen = fresh, unixSeconds(a), idSet(sessions)
	} else {
		groups, at = saved.groups, saved.at
		var fresh []session
		for _, s := range sessions {
			if !seen[s.ID] {
				fresh = append(fresh, s)
			}
		}
		if len(fresh) > 0 {
			placed, answered, err := a.assignSessions(groups, fresh)
			if err != nil {
				return nil, err
			}
			if answered { // a call that never answered leaves the new sessions for the next run
				groups = placed
				for _, s := range fresh {
					seen[s.ID] = true
				}
			}
		}
	}
	list := make([]any, len(groups))
	for i, g := range groups {
		list[i] = g
	}
	record := objOf("at", at, "groups", list, "seen", sortedKeys(seen))
	if err := writePrivate(a.cachePath(groupsFile), dumpJSONText(record)); err != nil {
		return nil, err
	}
	return groups, nil
}

func median(values []int) int {
	sorted := append([]int{}, values...)
	sort.Ints(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// mostFired is the skill fired in the most sessions, and in how many.
func mostFired(ss []session) (string, int) {
	counts := map[string]int{}
	var order []string
	for _, s := range ss {
		for _, name := range s.skillList() {
			if counts[name] == 0 {
				order = append(order, name)
			}
			counts[name]++
		}
	}
	top, n := "", 0
	for _, name := range order {
		if counts[name] > n {
			top, n = name, counts[name]
		}
	}
	return top, n
}

// kindsOfWork turns the groups into kinds, with what their sessions add up to.
func kindsOfWork(groups []*obj, sessions []session) []kind {
	byID := make(map[string]session, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}
	var kinds []kind
	for _, g := range groups {
		var ss []session
		for _, id := range stringList(g.val(sessionIDsKey)) {
			if s, ok := byID[id]; ok {
				ss = append(ss, s)
			}
		}
		if len(ss) == 0 {
			continue
		}
		k := kind{Name: oneLine(valueText(g, "name")), Task: oneLine(valueText(g, "task")),
			Delegable: g.val("delegable") == true, Queries: searchQueries(g), MCPQueries: stringList(g.val("mcp_queries")),
			Sessions: len(ss)}
		weeks := map[string]bool{}
		tokens := make([]int, len(ss))
		for i, s := range ss {
			k.IDs = append(k.IDs, s.ID)
			weeks[week(s.TS)] = true
			tokens[i] = s.Tokens
			k.Tokens += s.Tokens
			k.Friction += s.Errors + interruptWeight*s.Interrupts
			if i < examplesPerKind && len(s.Prompts) > 0 {
				k.Examples = append(k.Examples, cut(s.Prompts[0], exampleChars))
			}
		}
		k.Weeks, k.MedianTokens = len(weeks), median(tokens)
		if top, n := mostFired(ss); n > 0 && float64(n)/float64(len(ss)) >= coveredShare {
			k.CoveredBy = top // a skill you use in most of these sessions
		}
		kinds = append(kinds, k)
	}
	return kinds
}

func gapScore(k kind) float64 {
	sessions := float64(k.Sessions)
	return sessions * (1 + float64(k.Friction)/sessions) * (1 + float64(k.MedianTokens)/tokenScale)
}

// findGaps gives the recurring, delegable kinds of work that no skill covers,
// used or merely installed (that is `reap reuse`).
func (a *app) findGaps(kinds []kind, installed []skill) ([]kind, error) {
	var open []kind
	for _, k := range kinds {
		if k.Sessions >= minSessions && k.Delegable && k.Task != "" && k.CoveredBy == "" {
			open = append(open, k)
		}
	}
	covering, answered, err := a.coveringSkills(open, installed)
	if err != nil {
		return nil, err
	}
	if !answered { // not knowing what covers this work is not the same as nothing covering it
		a.failed("checking your installed skills got no answer · nothing proposed this run")
		return nil, nil
	}
	var gaps []kind
	for i, k := range open {
		if covering[i] == "" {
			gaps = append(gaps, k)
		}
	}
	sort.SliceStable(gaps, func(i, j int) bool { return gapScore(gaps[i]) > gapScore(gaps[j]) })
	return gaps[:min(maxGaps, len(gaps))], nil
}
