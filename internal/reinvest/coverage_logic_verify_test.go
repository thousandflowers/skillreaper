package reinvest

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// Candidates, verdicts, swaps and the report: orderings, refusals and the
// failures the other tests leave out.

const logicCovDesc = "a description long enough to pass the length gate here"

func logicCovDoc(extra string) string {
	return "---\nname: x\ndescription: " + logicCovDesc + "\n" + extra + "---\nbody"
}

func logicCovNames(cands []candidate) string { return strings.Join(candNames(cands), ",") }

// --- candidates.go -------------------------------------------------------------

func TestLogicCov_rankedSkills_orders_by_hits_then_place_then_installs(t *testing.T) {
	gap := kind{Queries: []string{"a", "b"}}
	found := []candidate{
		{ID: "late", Name: "late", Installs: 900, Hits: map[string]int{"a": 7}},
		{ID: "early", Name: "early", Installs: 30, Hits: map[string]int{"a": 2, "elsewhere": 0}}, // another gap's search does not count
		{ID: "both", Name: "both", Installs: 20, Hits: map[string]int{"a": 9, "b": 9}},
		{ID: "popular", Name: "popular", Installs: 5000, Hits: map[string]int{"b": 2}},
		{ID: "few", Name: "few", Installs: minInstalls - 1, Hits: map[string]int{"a": 0, "b": 0}},
		{ID: "off", Name: "off", Installs: 9000, Hits: map[string]int{"elsewhere": 0}},
	}
	if got := logicCovNames(rankedSkills(gap, found)); got != "both,popular,early,late" {
		t.Errorf("ranked %s", got)
	}
}

func TestLogicCov_rankedSkills_keeps_candidatesPerGap(t *testing.T) {
	var found []candidate
	for i := 0; i < candidatesPerGap+5; i++ {
		found = append(found, candidate{ID: string(rune('A' + i)), Name: string(rune('A' + i)), Installs: 100, Hits: map[string]int{"q": i}})
	}
	got := rankedSkills(kind{Queries: []string{"q"}}, found)
	if len(got) != candidatesPerGap || got[0].Name != "A" || got[candidatesPerGap-1].Name != string(rune('A'+candidatesPerGap-1)) {
		t.Errorf("kept %s", logicCovNames(got))
	}
}

func TestLogicCov_candidatesFor_refuses_and_names(t *testing.T) {
	a := fakeApp(t)
	found := []candidate{
		{ID: "o/r/short", Slug: "short", Name: "short", Installs: 50, Hits: map[string]int{"q": 0}},
		{ID: "o/r/manual", Slug: "manual", Name: "manual", Installs: 50, Hits: map[string]int{"q": 1}},
		{ID: "o/r/good", Slug: "good", Name: "good", Installs: 50, Hits: map[string]int{"q": 2}},
	}
	a.skillDoc = func(c candidate) string {
		switch c.Slug {
		case "short":
			return "---\ndescription: too short\n---\n"
		case "manual":
			return logicCovDoc("disable-model-invocation: True\n")
		}
		return logicCovDoc("")
	}
	a.searchMCP = func([]string) []candidate {
		return []candidate{
			{ID: "io.github.acme/files", Name: "Files", Kind: kindMCP, Doc: "tools: list_files", Cost: intPtr(5)},
			{ID: "plain", Name: "Plain", Kind: kindMCP},
			{ID: "same/name", Name: "same/name", Kind: kindMCP},
		}
	}
	got, refused := a.candidatesFor(kind{Name: "G", Sessions: 3, Task: "the task", Queries: []string{"q"}}, found, false)
	if names := logicCovNames(got); names != "good,Files (io.github.acme),Plain,same/name" {
		t.Errorf("candidates %s", names)
	}
	for _, c := range got {
		if c.Gap != "G (3 sessions)" || c.Task != "the task" {
			t.Errorf("%s: gap %q task %q", c.Name, c.Gap, c.Task)
		}
		if (c.Kind == kindMCP) != (c.Cost == nil) {
			t.Errorf("%s: cost %v (an MCP server's weight is unknown until it runs)", c.Name, c.Cost)
		}
	}
	if got[0].Desc != logicCovDesc || got[0].Cost == nil || *got[0].Cost != reapTokens("good", got[0].Doc) {
		t.Errorf("the skill was not weighed as reap will: %+v", got[0])
	}
	if len(refused) != 1 || refused[0] != (refusal{"o/r/manual", "manual", manualOnlyReason}) {
		t.Errorf("refused %+v", refused)
	}
}

// --- verify.go -----------------------------------------------------------------

func TestLogicCov_verdictJSON_only_an_object_counts(t *testing.T) {
	for _, out := range []string{`[{"risk": "none"}]`, `"none"`, "```json\n5\n```", `null`} {
		if got := verdictJSON(out); got != nil {
			t.Errorf("verdictJSON(%q) = %v", out, got)
		}
	}
	got, ok := verdictJSON("```json\n{\"risk\": \"none\", \"keep\": true}\n```").(*obj)
	if !ok || got.val("keep") != true {
		t.Errorf("a fenced object was not read: %v", got)
	}
}

func TestLogicCov_verifyOne_an_error_from_Claude_fails_closed(t *testing.T) {
	a := fakeApp(t)
	logicCovAskFails(a)
	v, err := a.verifyOne(verifyRequest{Task: "t", Doc: "a harmless document"})
	if !errors.Is(err, errLogicCovBoom) || v != failedVerdict {
		t.Errorf("got %+v, %v", v, err)
	}
}

// --- verifyall.go --------------------------------------------------------------

func TestLogicCov_usableVerdict_refuses_a_damaged_record(t *testing.T) {
	now := 1000.0
	cases := []struct {
		record string
		ok     bool
	}{
		{`{"keep": "yes", "reason": "r", "at": 999}`, false},
		{`{"keep": false, "reason": 5, "at": 999}`, false},
		{`{"keep": false, "at": 999}`, true}, // no reason is an empty one
		{`{"keep": true, "fit": 3, "use": 5, "reason": "r", "at": 999}`, false},
		{`{"keep": true, "fit": 3.0, "reason": "r", "at": 999}`, false},
		{`{"keep": true, "fit": 3, "reason": "r", "at": "999"}`, false},
		{`{"keep": true, "fit": 3, "reason": "r", "at": -604801000}`, false},
		{`{"keep": true, "fit": 3, "reason": "r", "risk": "none", "at": 999}`, true},
	}
	for _, c := range cases {
		if _, ok := usableVerdict(js(t, c.record), now); ok != c.ok {
			t.Errorf("usableVerdict(%s) ok = %v, want %v", c.record, ok, c.ok)
		}
	}
	if _, ok := usableVerdict("not an object", now); ok {
		t.Error("a string was read as a verdict")
	}
}

var logicCovPlain = candidate{ID: "o/r/s", Kind: kindSkill, Name: "s", Source: "o/r", Slug: "s", Doc: "a doc", Task: "t", Gap: "g"}

func TestLogicCov_verifyAll_a_verifier_error_stops_the_run(t *testing.T) {
	a := fakeApp(t)
	a.verify = func(verifyRequest) (verdict, error) { return verdict{}, errLogicCovBoom }
	if _, _, err := a.verifyAll([]candidate{logicCovPlain}, nil, nil); !errors.Is(err, errLogicCovBoom) {
		t.Errorf("got %v", err)
	}
}

func TestLogicCov_verifyAll_reports_a_cache_it_cannot_write(t *testing.T) {
	t.Run("verdicts", func(t *testing.T) {
		a := fakeApp(t)
		a.cacheDir = logicCovNotADir(t)
		verifying(a, always(verdict{Keep: true, Risk: noRisk, Fit: 3}))
		if _, _, err := a.verifyAll([]candidate{logicCovPlain}, nil, nil); err == nil {
			t.Error("no error")
		}
	})
	t.Run("denied documents", func(t *testing.T) {
		a := fakeApp(t)
		if err := os.Mkdir(a.cachePath(deniedFile), 0o755); err != nil { // a folder where the file goes
			t.Fatal(err)
		}
		verifying(a, always(verdict{Keep: true, Risk: noRisk, Fit: 3}))
		if _, _, err := a.verifyAll([]candidate{logicCovPlain}, nil, nil); err == nil {
			t.Error("no error")
		}
	})
}

func TestLogicCov_joinReasons_once_each_most_serious_first(t *testing.T) {
	got := joinReasons([]string{"b other", failedReason, securityPrefix + "x", "a other", "b other", securityPrefix + "y"})
	want := "security: x; security: y; b other; a other; " + failedReason
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestLogicCov_refusalLines_a_later_refusal_replaces_in_place(t *testing.T) {
	got := refusalLines([]refusal{{"a", "A", "r1"}, {"b", "B", "r2"}, {"a", "A2", "r3"}})
	if len(got) != 2 || got[0] != (rejection{"A2", "r3"}) || got[1] != (rejection{"B", "r2"}) {
		t.Errorf("got %+v", got)
	}
	if got := refusalLines(nil); got == nil || len(got) != 0 {
		t.Errorf("no refusals: %#v", got)
	}
}

func TestLogicCov_bestPerGap_on_a_tie_the_lighter_and_known_weight_wins(t *testing.T) {
	passing := []candidate{
		{Name: "unknown", Gap: "g1", Fit: 4},
		{Name: "heavy", Gap: "g1", Fit: 4, Cost: intPtr(500)},
		{Name: "light", Gap: "g1", Fit: 4, Cost: intPtr(100)},
		{Name: "weak", Gap: "g1", Fit: 2, Cost: intPtr(1)},
		{Name: "light", Gap: "g2", Fit: 3, Cost: intPtr(1)}, // the same name for another gap: once is enough
		{Name: "other", Gap: "g2", Fit: 3, Cost: intPtr(9)},
		{Name: "third", Gap: "g3", Fit: 1},
	}
	if got := logicCovNames(bestPerGap(passing, 9)); got != "light,other,third" {
		t.Errorf("picked %s", got)
	}
	if got := logicCovNames(bestPerGap(passing, 1)); got != "light" {
		t.Errorf("picked %s with a limit of 1", got)
	}
	if got := logicCovNames(bestPerGap([]candidate{{Name: "known", Gap: "g", Fit: 2, Cost: intPtr(900)}, {Name: "unknown", Gap: "g", Fit: 2}}, 1)); got != "known" {
		t.Errorf("an unknown weight won a tie: %s", got)
	}
	if got := logicCovNames(bestPerGap([]candidate{{Name: "unknown", Gap: "g", Fit: 2}, {Name: "known", Gap: "g", Fit: 2, Cost: intPtr(900)}}, 1)); got != "known" {
		t.Errorf("an unknown weight won a tie: %s", got)
	}
}

// --- swaps.go ------------------------------------------------------------------

func logicCovSkill(name string, tokens int) skill { return skill{Name: name, Tokens: tokens} }

func skillNamesOf(skills []skill) string {
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.Name
	}
	return strings.Join(names, ",")
}

func TestLogicCov_pair_falls_back_to_the_heaviest_few(t *testing.T) {
	pool := []skill{logicCovSkill("s100", 100), logicCovSkill("s300", 300), logicCovSkill("s200", 200), logicCovSkill("s50", 50)}
	// no single or pair comes to 70% of 1000: the heaviest three, as long as they are short of it
	if got := skillNamesOf(pair(intPtr(1000), pool)); got != "s300,s200,s100" {
		t.Errorf("1000: %s", got)
	}
	// the first two still fall short of 1000, so the third is added too
	if got := skillNamesOf(pair(intPtr(1000), []skill{logicCovSkill("a", 350), logicCovSkill("b", 300), logicCovSkill("c", 50)})); got != "a,b,c" {
		t.Errorf("650 of 1000: %s", got)
	}
	// a pair nearer the cost than any single wins: 510 is 90 from 600, 500 is 100
	if got := skillNamesOf(pair(intPtr(600), []skill{logicCovSkill("a", 500), logicCovSkill("b", 400), logicCovSkill("c", 10)})); got != "a,c" {
		t.Errorf("600: %s", got)
	}
	if got := pair(intPtr(500), nil); len(got) != 0 {
		t.Errorf("an empty pool: %v", got)
	}
	if got := pair(nil, pool); got != nil {
		t.Errorf("an unknown weight: %v", got)
	}
}

func TestLogicCov_pair_heaviest_stops_once_the_weight_is_reached(t *testing.T) {
	// 69 is short of 70% of 100, 69 + 68 is over the 130% a pair may weigh, and 69 + 0 is no nearer than 69:
	// the heaviest are taken until they reach 100, and the third is not needed
	pool := []skill{logicCovSkill("a", 69), logicCovSkill("b", 68), logicCovSkill("c", 0)}
	if got := skillNamesOf(pair(intPtr(100), pool)); got != "a,b" {
		t.Errorf("100: %s", got)
	}
	// with a pair in reach, the closest pair wins over the closest single
	if got := skillNamesOf(pair(intPtr(150), []skill{logicCovSkill("a", 100), logicCovSkill("b", 90), logicCovSkill("c", 80)})); got != "b,c" {
		t.Errorf("150: %s", got)
	}
}

func TestLogicCov_dropProposals_without_a_file_writes_nothing(t *testing.T) {
	a := fakeApp(t)
	if err := a.dropProposals(nil); err != nil {
		t.Fatal(err)
	}
	if exists(a.cachePath(swapsFile)) {
		t.Error("a proposals file was made from nothing")
	}
}

func TestLogicCov_build_orders_each_swaps_candidates(t *testing.T) {
	passing := []candidate{
		{Name: "elsewhere", Gap: "g2", Fit: 5, Cost: intPtr(100)},
		{Name: "unknown", Gap: "g1", Fit: 3},
		{Name: "far", Gap: "g1", Fit: 3, Cost: intPtr(400)},
		{Name: "near", Gap: "g1", Fit: 3, Cost: intPtr(120)},
		{Name: "best", Gap: "g1", Fit: 4, Cost: intPtr(999)},
	}
	picks := []candidate{
		passing[3],
		{Name: "nothing-to-trade", Gap: "g3", Cost: intPtr(5000)},     // the pool is used up: nothing to trade, skipped
		{Name: "written", Gap: "g1", Created: true, Cost: intPtr(50)}, // nothing left to mute: offered anyway
	}
	swaps := build(picks, passing, []skill{logicCovSkill("old", 100)})
	if len(swaps) != 2 {
		t.Fatalf("%d swaps", len(swaps))
	}
	first := swaps[0]
	if logicCovNames(first.Cands) != "best,near,far,unknown,elsewhere" || first.cand().Name != "near" || first.Out != 100 ||
		skillNamesOf(first.Mute) != "old" {
		t.Errorf("first swap: %s, pick %s, out %d", logicCovNames(first.Cands), first.cand().Name, first.Out)
	}
	second := swaps[1]
	if len(second.Mute) != 0 || second.Idx != 0 { // "written" is not among the passing: the first is shown
		t.Errorf("second swap: %+v", second)
	}
}

func TestLogicCov_closer_ranks_gap_then_fit_then_known_weight_then_distance(t *testing.T) {
	c := func(gap string, fit int, cost *int) candidate { return candidate{Gap: gap, Fit: fit, Cost: cost} }
	cases := []struct {
		a, b candidate
		want bool
	}{
		{c("g", 1, nil), c("x", 5, intPtr(0)), true},
		{c("x", 5, intPtr(0)), c("g", 1, nil), false},
		{c("g", 4, nil), c("g", 3, intPtr(100)), true},
		{c("g", 3, intPtr(900)), c("g", 3, nil), true},
		{c("g", 3, nil), c("g", 3, intPtr(900)), false},
		{c("g", 3, intPtr(110)), c("g", 3, intPtr(80)), true}, // 10 from 100 against 20
		{c("g", 3, intPtr(80)), c("g", 3, intPtr(120)), false},
		{c("x", 3, intPtr(100)), c("y", 3, intPtr(100)), false},
	}
	for i, k := range cases {
		if got := closer(k.a, k.b, "g", 100); got != k.want {
			t.Errorf("case %d: closer = %v", i, got)
		}
	}
}

func TestLogicCov_muteText_and_names(t *testing.T) {
	unknown := swap{Cands: []candidate{{Name: "srv"}}}
	if got := muteText(unknown); got != "nothing · the weight of what it installs is unknown, so nothing is traded for it" {
		t.Errorf("unknown weight: %q", got)
	}
	if got := muteText(swap{Cands: []candidate{{Cost: intPtr(5)}}}); got != "nothing · no unused skill of about its weight" {
		t.Errorf("known weight: %q", got)
	}
	if got := muteText(swap{Cands: []candidate{{}}, Held: []skill{logicCovSkill("h", 1)}}); got != "nothing · m to mute h again" {
		t.Errorf("held: %q", got)
	}
	five := []skill{logicCovSkill("a", 1), logicCovSkill("b", 1), logicCovSkill("c", 1), logicCovSkill("d", 1), logicCovSkill("e", 1)}
	if got := muteText(swap{Cands: []candidate{{}}, Mute: five, Out: 5}); got != "a + b + c + 2 more   ~5 tok/session · never used" {
		t.Errorf("five muted: %q", got)
	}
	if got := names(nil); got != "nothing" {
		t.Errorf("names(nil) = %q", got)
	}
}

func TestLogicCov_mcpName_prefers_the_registry_name(t *testing.T) {
	if got := mcpName(candidate{Name: "Files (acme)", Server: objOf("name", "io.github.acme/files")}); got != "files" {
		t.Errorf("got %q", got)
	}
	if got := mcpName(candidate{Name: "io.github.acme/docs"}); got != "docs" {
		t.Errorf("got %q", got)
	}
	if got := mcpName(candidate{Name: "io.github.acme/docs", Server: objOf("name", 5)}); got != "docs" {
		t.Errorf("a name that is not text: %q", got)
	}
}

func TestLogicCov_settle_skips_damaged_entries_and_reports_a_failed_write(t *testing.T) {
	a := fakeApp(t)
	writeFile(t, a.cachePath(weighLaterFile), `{"broken": 5, "docs": {"name": "Docs", "gap": "g", "why": "w", "fit": "4"}, "gone": {}}`)
	weights := map[string]*int{"broken": intPtr(100), "docs": intPtr(100)}
	out, err := a.settle(weights, []skill{logicCovSkill("old", 100), logicCovSkill("older", 100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].cand().Name != "Docs" || out[0].cand().Fit != 4 || skillNamesOf(out[0].Mute) != "old" {
		t.Errorf("swaps %+v", out)
	}
	if saved := readFile(t, a.cachePath(weighLaterFile)); strings.Contains(saved, "gone") {
		t.Errorf("a server reap no longer lists is still remembered: %s", saved)
	}

	b := fakeApp(t)
	b.cacheDir = logicCovNotADir(t)
	if _, err := b.settle(nil, nil); err == nil {
		t.Error("a list that could not be saved was not reported")
	}
}

// --- report.go -----------------------------------------------------------------

func TestLogicCov_thousands(t *testing.T) {
	cases := map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", -1000: "-1,000", 1234567: "1,234,567", -12: "-12", 100000: "100,000"}
	for n, want := range cases {
		if got := thousands(n); got != want {
			t.Errorf("thousands(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestLogicCov_summary_names_the_gaps_with_no_fit(t *testing.T) {
	a := fakeApp(t)
	solved := kind{Name: "Releases", Sessions: 4}
	cand := candidate{Name: "ship", Kind: kindSkill, Source: "o/r", Gap: solved.gapLabel(), Why: "it ships", Cost: intPtr(30)}
	s := swap{Mute: []skill{logicCovSkill("old", 30)}, Out: 30, Cands: []candidate{cand}}
	out := a.summary([]swap{s}, []kind{solved, {Name: "Docs", Sessions: 3}, {Name: "Lint", Sessions: 5}}, 12345)
	for _, part := range []string{"~12,345 dead tok/session", "mute      old   ~30 tok/session · never used",
		"install   ship   ~30 tok/session · skill · o/r", "for Releases (4 sessions)", "it ships",
		"no fit found for: Docs, Lint", "details: " + a.reportPath()} {
		if !strings.Contains(out, part) {
			t.Errorf("the summary does not say %q:\n%s", part, out)
		}
	}
	if strings.Contains(a.summary(nil, nil, 0), "no fit found") {
		t.Error("no gaps, yet a gap with no fit")
	}
}

func TestLogicCov_gapName_drops_only_the_count(t *testing.T) {
	cases := map[string]string{"plain": "plain", "Docs (3 sessions)": "Docs", "a (b) (3 sessions)": "a (b)", "": ""}
	for in, want := range cases {
		if got := gapName(in); got != want {
			t.Errorf("gapName(%q) = %q", in, got)
		}
	}
}

func TestLogicCov_writeReport(t *testing.T) {
	a := fakeApp(t)
	kinds := []kind{{Name: "Releases", Sessions: 4, Weeks: 2, CoveredBy: "ship-it"}, {Name: "Docs", Sessions: 3, Weeks: 1}}
	passing := []candidate{{ID: "o/r/x", Name: "x", Kind: kindMCP, Gap: "Docs (3 sessions)", Fit: 3, Why: "w"}}
	if err := a.writeReport(kinds, kinds[1:], passing, []rejection{{"y", "too thin"}}); err != nil {
		t.Fatal(err)
	}
	report := readFile(t, a.reportPath())
	for _, part := range []string{"- Releases: 4 sessions, 2 weeks, covered by ship-it\n", "- Docs: 3 sessions, 1 weeks\n",
		"- x (MCP server, weight unknown until installed, fit 3/5) for Docs (3 sessions): w", "- y: too thin\n"} {
		if !strings.Contains(report, part) {
			t.Errorf("the report does not say %q:\n%s", part, report)
		}
	}
	want := `[{"id": "o/r/x", "name": "x", "gap": "Docs", "fit": 3, "why": "w", "cost": null, "installs": 0}]`
	if got := readFile(t, a.cachePath(verifiedFile)); got != want {
		t.Errorf("verified.json:\n%s\nwant\n%s", got, want)
	}

	b := fakeApp(t)
	b.cacheDir = logicCovNotADir(t)
	if err := b.writeReport(nil, nil, nil, nil); err == nil {
		t.Error("a report that could not be written was not reported")
	}
}

func TestLogicCov_refusalNotes_count_failed_checks_apart(t *testing.T) {
	a := fakeApp(t)
	notes := a.refusalNotes([]rejection{
		{"a", failedReason}, {"b\x1b[31m", securityPrefix + "downloads and runs code\x07"}, {"c", "too thin; " + failedReason}, {"d", "thin"},
	})
	want := []string{
		"  refused for security: b[31m: security: downloads and runs code",
		"  the check could not run for 2 · they count as not checked, not as no fit",
		"  details: " + a.reportPath(),
	}
	equalStrings(t, notes, want)
	if got := a.refusalNotes(nil); got != nil {
		t.Errorf("no refusals: %q", got)
	}
}

// --- tokens.go and text.go -----------------------------------------------------

func TestLogicCov_description_inline_values(t *testing.T) {
	cases := []struct {
		header []string
		want   string
	}{
		{[]string{`description: "bad \x escape"`}, `bad \x escape`}, // not JSON: the quotes are trimmed
		{[]string{`description: "a" "b"`}, `a" "b`},
		{[]string{`description: 'single'`, "  not joined"}, "single"},
		{[]string{`description: "open`, "  not joined"}, "open"},
		{[]string{`description: plain`, "  and more", "", "\tlast", "next: key"}, "plain and more last"},
		{[]string{`description:`}, ""},
		{[]string{"  description: indented", "description: top"}, "top"},
		{[]string{"name: x"}, ""},
	}
	for _, c := range cases {
		if got := description(c.header); got != c.want {
			t.Errorf("description(%q) = %q, want %q", c.header, got, c.want)
		}
	}
}

func TestLogicCov_block_scalars(t *testing.T) {
	cases := []struct {
		lines        []string
		style, chomp string
		want         string
	}{
		{[]string{"    deep", "  a"}, "|", "", "deep\na\n"}, // shorter than the indent: stripped, as reap does
		{[]string{"  a", "", ""}, "|", "+", "a\n\n\n"},
		{[]string{"  a", "", ""}, "|", "-", "a"},
		{[]string{"  a", "  b", "", "  c", "", "", "  d", ""}, ">", "", "a b\nc\n\nd\n"},
		{[]string{"", "  a", "  b"}, ">", "-", "\na b"}, // a leading empty line stays a line break
		{[]string{"", "   "}, "|", "", ""},
	}
	for _, c := range cases {
		if got := block(c.lines, c.style, c.chomp); got != c.want {
			t.Errorf("block(%q, %s%s) = %q, want %q", c.lines, c.style, c.chomp, got, c.want)
		}
	}
}

func TestLogicCov_frontmatter_a_blank_line_does_not_continue_a_value(t *testing.T) {
	got := frontmatter("---\ndescription: first\n\n  second\nname: n\n---\n")
	if got["description"] != "first second" || got["name"] != "n" {
		t.Errorf("got %q", got)
	}
	if startsWithSpace("") || !startsWithSpace("\u3000x") || startsWithSpace("x ") {
		t.Error("startsWithSpace")
	}
}
