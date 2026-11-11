package reinvest

import (
	"sort"
	"strings"
)

const (
	verdictsTTL   = 7 * day // a verdict is keyed by everything it read, so it only goes stale with the candidate
	verdictsFile  = "verdicts.json"
	deniedFile    = "denied.json" // documents a verifier flagged, by hash: a risk belongs to the text, whatever the task
	verifyRuns    = 2             // a candidate is kept only if every run keeps it: borderline ones flipped between runs
	verifyWorkers = 8
	noSafeInstall = "no safe way to install it"
)

// Which reason to keep: a security finding first, a call that never answered last.
const (
	rankSecurity = iota
	rankOther
	rankFailed
)

func reasonRank(reason string) int {
	switch {
	case strings.HasPrefix(reason, securityPrefix):
		return rankSecurity
	case reason == failedReason:
		return rankFailed
	}
	return rankOther
}

// combine turns several verdicts on one candidate into one: kept only if
// every run kept it, with the lowest fit. A security finding in any run is
// the reason, whatever the other runs said, and so it is cached.
func combine(runs []verdict) verdict {
	worst, rejected := "", false
	for _, v := range runs {
		if v.Keep {
			continue
		}
		if !rejected || reasonRank(v.Reason) < reasonRank(worst) {
			worst = v.Reason
		}
		rejected = true
	}
	if rejected {
		return verdict{Keep: false, Reason: worst}
	}
	out := runs[0]
	for _, v := range runs {
		if v.Fit < minFit || v.Fit > maxFit {
			return failedVerdict
		}
		out.Fit = min(out.Fit, v.Fit)
	}
	return out
}

// usableVerdict reads a cached verdict. One that is damaged, not ours or
// older than a week is not used.
func usableVerdict(raw any, now float64) (verdict, bool) {
	v, ok := raw.(*obj)
	if !ok {
		return verdict{}, false
	}
	at, isNumber := number(v.val("at"))
	if !isNumber || now-at >= verdictsTTL.Seconds() {
		return verdict{}, false
	}
	reason, okReason := textOr(v, "reason")
	keep, isBool := v.val("keep").(bool)
	if !isBool || !okReason {
		return verdict{}, false
	}
	if !keep {
		return verdict{Keep: false, Reason: reason, At: at}, true
	}
	fit, okFit := validFit(v.val("fit"))
	use, okUse := textOr(v, "use")
	if !okFit || !okUse {
		return verdict{}, false
	}
	risk, _ := v.str("risk")
	return verdict{Keep: true, Risk: risk, Fit: fit, Reason: reason, Use: use, At: at}, true
}

// verdictRecord is a verdict as verdicts.json holds it.
func verdictRecord(v verdict) *obj {
	if !v.Keep {
		return objOf("keep", false, "reason", v.Reason, "at", v.At)
	}
	record := objOf("keep", true)
	if v.Risk != "" {
		record.set("risk", v.Risk)
	}
	record.set("fit", v.Fit)
	record.set("reason", v.Reason)
	record.set("use", v.Use)
	record.set("at", v.At)
	return record
}

// identity is who a candidate says it is and what installing it runs: the
// verifier checks these against the document.
func identity(c candidate) string {
	switch {
	case c.Created:
		return ""
	case c.Kind == kindMCP:
		return dumpJSON(objOf("registry name", c.ID, "installs", targetOf(c.Server).asJSON()))
	}
	return "named " + c.Name + ", from the repo " + c.Source + ", folder " + c.Slug
}

func docHash(doc string) string { return sha256Hex(doc) }

func requestFor(c candidate, used, installed []skill) verifyRequest {
	return verifyRequest{Task: c.Task, Doc: c.Doc, Used: used, Installed: installed, Own: c.Created, About: identity(c)}
}

// loadVerdicts reads the verdict cache: what the file held for each usable
// entry, and the verdicts themselves.
func (a *app) loadVerdicts(now float64) (*obj, map[string]verdict) {
	kept, verdicts := newObj(), map[string]verdict{}
	saved, _ := readJSONFile(a.cachePath(verdictsFile)).(*obj)
	for _, key := range saved.keyList() {
		if v, ok := usableVerdict(saved.val(key), now); ok {
			kept.set(key, saved.val(key))
			verdicts[key] = v
		}
	}
	return kept, verdicts
}

// loadDenied reads the documents flagged before: hash -> reason.
func (a *app) loadDenied() *obj {
	denied := newObj()
	saved, _ := readJSONFile(a.cachePath(deniedFile)).(*obj)
	for _, key := range saved.keyList() {
		if reason, ok := saved.str(key); ok {
			denied.set(key, reason)
		}
	}
	return denied
}

type verifyRun struct {
	verdict verdict
	err     error
}

// runVerifier asks for every missing verdict, verifyRuns times each, and
// combines the runs per key.
func (a *app) runVerifier(todo []string, requests map[string]verifyRequest) (map[string]verdict, error) {
	var jobs []verifyRequest
	for _, key := range todo {
		for range verifyRuns {
			jobs = append(jobs, requests[key])
		}
	}
	runs := parallelMap(jobs, verifyWorkers, func(req verifyRequest) verifyRun {
		v, err := a.verify(req)
		return verifyRun{v, err}
	})
	out := map[string]verdict{}
	for i, key := range todo {
		mine := make([]verdict, 0, verifyRuns)
		for _, run := range runs[i*verifyRuns : (i+1)*verifyRuns] {
			if run.err != nil {
				return nil, run.err
			}
			mine = append(mine, run.verdict)
		}
		out[key] = combine(mine)
	}
	return out, nil
}

// verifyAll verifies each candidate verifyRuns times; verdicts are cached by
// everything the verifier read. A document flagged once stays flagged: one
// lucky roll on another task or another day must not pass it.
func (a *app) verifyAll(cands []candidate, used, installed []skill) ([]candidate, []rejection, error) {
	now := unixSeconds(a)
	cache, cached := a.loadVerdicts(now)
	denied := a.loadDenied()
	keys := make([]string, len(cands))
	verdicts := map[string]verdict{}
	requests := map[string]verifyRequest{}
	var todo []string
	for i, c := range cands {
		req := requestFor(c, used, installed)
		keys[i] = a.verifyKey(req)
		if v, ok := cached[keys[i]]; ok {
			verdicts[keys[i]] = v
			continue
		}
		if _, isDenied := denied.get(docHash(c.Doc)); isDenied {
			continue
		}
		if _, queued := requests[keys[i]]; !queued {
			todo = append(todo, keys[i])
		}
		requests[keys[i]] = req
	}
	fresh, err := a.runVerifier(todo, requests)
	if err != nil {
		return nil, nil, err
	}
	for _, key := range todo {
		v := fresh[key]
		v.At = now
		verdicts[key] = v
		if v.Reason != failedReason { // a call that never answered is not a verdict: not cached
			cache.set(key, verdictRecord(v))
		}
	}
	if err := writePrivate(a.cachePath(verdictsFile), dumpJSON(cache)); err != nil {
		return nil, nil, err
	}
	for i, c := range cands {
		reason := verdicts[keys[i]].Reason
		if _, known := denied.get(docHash(c.Doc)); !known && strings.HasPrefix(reason, securityPrefix) {
			denied.set(docHash(c.Doc), reason)
		}
	}
	if err := writePrivate(a.cachePath(deniedFile), dumpJSON(denied)); err != nil {
		return nil, nil, err
	}
	passing, rejected := sortVerdicts(cands, keys, verdicts, denied)
	return passing, rejected, nil
}

// sortVerdicts splits the candidates into those that pass and, per candidate,
// the reasons against the others.
func sortVerdicts(cands []candidate, keys []string, verdicts map[string]verdict, denied *obj) ([]candidate, []rejection) {
	var passing []candidate
	reasons := map[string][]string{}
	names := map[string]string{}
	var order []string
	for i, c := range cands {
		v := verdicts[keys[i]]
		deniedReason, isDenied := denied.str(docHash(c.Doc))
		reason := v.Reason
		if deniedReason != "" {
			reason = deniedReason
		}
		if c.Kind == kindMCP && !targetOf(c.Server).found() {
			if reason == "" {
				reason = noSafeInstall
			}
			v = verdict{Keep: false}
		}
		if isDenied || !v.Keep {
			if _, seen := names[c.ID]; !seen {
				names[c.ID] = c.Name
				order = append(order, c.ID)
			}
			reasons[c.ID] = append(reasons[c.ID], reason)
			continue
		}
		if c.Kind == kindMCP {
			c.Where = mcpWhere(c.Server)
		}
		c.Fit, c.Why = v.Fit, v.Use
		if c.Why == "" {
			c.Why = v.Reason
		}
		passing = append(passing, c)
	}
	rejected := make([]rejection, len(order))
	for i, id := range order {
		rejected[i] = rejection{names[id], joinReasons(reasons[id])}
	}
	return passing, rejected
}

// joinReasons gives every reason once, the most serious first.
func joinReasons(reasons []string) string {
	seen := map[string]bool{}
	var unique []string
	for _, r := range reasons {
		if !seen[r] {
			seen[r] = true
			unique = append(unique, r)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return reasonRank(unique[i]) < reasonRank(unique[j]) })
	return strings.Join(unique, "; ")
}

// refusalLines turns refusals into report lines, one per candidate id.
func refusalLines(refused []refusal) []rejection {
	out := make([]rejection, 0, len(refused))
	at := map[string]int{}
	for _, r := range refused {
		if i, seen := at[r.ID]; seen { // a later refusal of the same id replaces the earlier one
			out[i] = rejection{r.Name, r.Reason}
			continue
		}
		at[r.ID] = len(out)
		out = append(out, rejection{r.Name, r.Reason})
	}
	return out
}

// bestPerGap picks the best fit for each gap; on a tie the lighter one (an
// unknown MCP weight ranks after any known one).
func bestPerGap(passing []candidate, limit int) []candidate {
	ranked := append([]candidate{}, passing...)
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		switch {
		case a.Fit != b.Fit:
			return a.Fit > b.Fit
		case (a.Cost == nil) != (b.Cost == nil):
			return b.Cost == nil
		}
		return a.costOrZero() < b.costOrZero()
	})
	var picks []candidate
	gaps, names := map[string]bool{}, map[string]bool{}
	for _, c := range ranked {
		if !gaps[c.Gap] && !names[c.Name] && len(picks) < limit {
			gaps[c.Gap], names[c.Name] = true, true
			picks = append(picks, c)
		}
	}
	return picks
}
