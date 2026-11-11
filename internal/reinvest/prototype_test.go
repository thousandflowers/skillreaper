package reinvest

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// The port held to the prototype itself: case by case, what the port answers
// against what the Python in experiments/reinvest answered for the same input.
// data/diff_cases.py asks the prototype and writes the cases.
//
// A sample of a few hundred cases is kept in data/sample_cases.jsonl and always
// runs. The full run, about a million cases and the real transcripts, needs
// the files that script writes, so those tests are skipped unless pointed at them:
//
//	python3 data/diff_cases.py cases ../../experiments/reinvest /tmp/cases.jsonl
//	REINVEST_DIFF_CASES=/tmp/cases.jsonl go test -run TestAgainstPrototype .

const (
	sampleCases   = "data/sample_cases.jsonl"
	shownPerKind  = 5
	longestCase   = 1 << 28
	firstCaseSize = 1 << 20
)

// compareCases runs every case in a file and reports, per kind, how many differ.
func compareCases(t *testing.T, path string) map[string]int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, firstCaseSize), longestCase)
	counts, failures := map[string]int{}, map[string]int{}
	for scanner.Scan() {
		v, err := parseJSON(scanner.Text())
		if err != nil {
			t.Fatalf("case does not parse: %v", err)
		}
		c := v.(*obj)
		kind, _ := c.str("k")
		counts[kind]++
		if msg := diffCase(kind, c.val("in"), c.val("want")); msg != "" {
			failures[kind]++
			if failures[kind] <= shownPerKind {
				t.Errorf("%s: %s", kind, msg)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for kind, n := range counts {
		t.Logf("%-16s %7d cases, %d differ", kind, n, failures[kind])
	}
	return counts
}

// TestPort_sample_of_the_prototypes_answers runs the sample kept in the repository.
func TestPort_sample_of_the_prototypes_answers(t *testing.T) {
	if counts := compareCases(t, sampleCases); len(counts) < 30 {
		t.Errorf("the sample holds only %d kinds of case", len(counts))
	}
}

// TestAgainstPrototype runs every case the script wrote.
func TestAgainstPrototype(t *testing.T) {
	path := os.Getenv("REINVEST_DIFF_CASES")
	if path == "" {
		t.Skip("set REINVEST_DIFF_CASES to the file `data/diff_cases.py cases` writes")
	}
	compareCases(t, path)
}

func runeSet(want any) map[rune]bool {
	out := map[rune]bool{}
	for _, v := range want.([]any) {
		out[rune(v.(int64))] = true
	}
	return out
}

func diffSet(want any, has func(rune) bool) string {
	set := runeSet(want)
	var wrong []string
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		if has(r) != set[r] && len(wrong) < 10 {
			wrong = append(wrong, hexPad(r, 4))
		}
	}
	return strings.Join(wrong, " ")
}

func anyStrings(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func mismatch(in, got, want any) string {
	if jsonEqual(got, want) {
		return ""
	}
	return "in " + dumpJSON(in) + "\n   got  " + dumpJSON(got) + "\n   want " + dumpJSON(want)
}

func diffCase(kind string, in, want any) string {
	text, _ := in.(string)
	switch kind {
	case "set_space", "set_space_re":
		return diffSet(want, isPySpace)
	case "set_word":
		return diffSet(want, isPyWord)
	case "set_digit":
		return diffSet(want, isPyDigit)
	case "set_cf":
		return diffSet(want, isFormatChar)
	case "set_linebreak":
		return diffSet(want, isLineBreak)
	case "set_printable":
		return diffSet(want, unicode.IsPrint)
	case "lower_diff":
		return diffLower(want.(*obj))
	case "fold":
		return diffFold(want.(*obj))
	case "risks":
		return mismatch(in, []any{anyStrings(risks(text, false)), anyStrings(risks(text, true))}, want)
	case "redact":
		return mismatch(in, redact(text), want)
	case "normalized":
		return mismatch(in, normalized(text), want)
	case "tool_form":
		return mismatch(in, toolForm(text), want)
	case "frontmatter":
		got := newObj()
		for k, v := range frontmatter(text) {
			got.set(k, v)
		}
		return mismatch(in, got, want)
	case "clean":
		return mismatch(in, clean(text), want)
	case "cut":
		return mismatch(in, cut(text, 12), want)
	case "canon":
		return mismatch(in, canon(text), want)
	case "one_line":
		return mismatch(in, oneLine(text), want)
	case "printable":
		return mismatch(in, printable(text), want)
	case "lower":
		return mismatch(in, pyLower(text), want)
	case "splitlines":
		return mismatch(in, anyStrings(pySplitLines(text)), want)
	case "fields":
		return mismatch(in, anyStrings(pyFields(text)), want)
	case "nfkc":
		return mismatch(in, nfkc(text), want)
	case "unescape":
		return mismatch(in, htmlUnescape(text), want)
	case "names_a_risk":
		return mismatch(in, namesARisk(text), want)
	case "search_queries":
		group, isGroup := in.(*obj)
		if !isGroup {
			return ""
		}
		return mismatch(in, anyStrings(searchQueries(group)), want)
	case "same_name":
		r := []rune(text)
		for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
			r[i], r[j] = r[j], r[i]
		}
		return mismatch(in, sameName(text, string(r)), want)
	case "words":
		var got []string
		for w := range words(text) {
			got = append(got, w)
		}
		sort.Strings(got)
		return mismatch(in, anyStrings(got), want)
	case "dumps":
		got := []any{dumpJSON(text), dumpJSONText(text), dumpJSON(objOf("a b", []any{text, int64(1), 1.5, nil, true}))}
		return mismatch(in, got, want)
	case "pystr":
		return mismatch(in, pyQuote(text), want)
	case "valid":
		got := objOf("source", sourceRE.matches(text), "slug", slugRE.matches(text), "mcp_name", mcpNameRE.matches(text),
			"package_id", packageIDRE.matches(text), "version", versionRE.MatchString(text), "subfolder", subfolderRE.MatchString(text),
			"remote", remoteRE.matches(text), "write_slug", writtenSlugRE.MatchString(text))
		return mismatch(in, got, want)
	case "hostname":
		return mismatch(in, mcpWhere(objOf("remotes", []any{objOf("type", "sse", "url", text)})), want)
	case "float":
		f, err := parseJSON(text)
		if err != nil {
			return "float does not parse: " + text
		}
		return mismatch(in, []any{pyRepr(f), dumpJSON(f)}, want)
	case "json":
		return diffJSON(text, want.(*obj))
	case "reap_tokens":
		pair := in.([]any)
		return mismatch(in, int64(reapTokens(pair[0].(string), pair[1].(string))), want)
	case "prompt":
		return mismatch(in, promptByName(text), want)
	case "decimal":
		return diffDecimals(want.(*obj))
	case "utf8":
		raw := in.([]any)
		b := make([]byte, len(raw))
		for i, v := range raw {
			b[i] = byte(v.(int64))
		}
		return mismatch(in, decodeUTF8Replace(b), want)
	case "shlex":
		got, err := shlexSplit(text)
		if err != nil {
			return mismatch(in, nil, want)
		}
		return mismatch(in, anyStrings(got), want)
	case "lenient":
		return mismatch(in, lenientJSON(text), want)
	case "verdict":
		return diffVerdict(in.([]any), want.(*obj))
	case "block":
		args := in.([]any)
		return mismatch(in, description(stringList(args)), want)
	case "pair":
		return diffPair(in.(*obj), want)
	case "week":
		return mismatch(in, week(text), want)
	case "verify_key":
		return diffVerifyKey(in.(*obj), want)
	case "thousands":
		return mismatch(in, thousands(int(in.(int64))), want)
	case "frame":
		return diffFrame(in.(*obj), want)
	case "keys":
		return diffKeys(in.(*obj), want)
	}
	return "unknown kind"
}

func candFrom(o *obj) candidate {
	c := candidate{Created: o.val("created") == true, Installed: o.val("installed") == true}
	c.Name, _ = o.str("name")
	c.Kind, _ = o.str("kind")
	c.Gap, _ = o.str("gap")
	c.Why, _ = o.str("why")
	c.Source, _ = o.str("source")
	c.Where, _ = o.str("where")
	c.Fit, _ = pyInt(o.val("fit"))
	if n, ok := o.val("cost").(int64); ok {
		c.Cost = intPtr(int(n))
	}
	return c
}

func swapsFrom(v any) []swap {
	var out []swap
	for _, raw := range v.([]any) {
		o := raw.(*obj)
		s := swap{Mute: skillsFrom(o.val("mute")), Held: skillsFrom(o.val("held")), OK: o.val("ok") == true}
		s.Out, _ = pyInt(o.val("out"))
		s.Idx, _ = pyInt(o.val("idx"))
		for _, c := range o.list("cands") {
			s.Cands = append(s.Cands, candFrom(c.(*obj)))
		}
		out = append(out, s)
	}
	return out
}

var attrNames = map[int]string{attrPlain: "plain", attrBold: "bold", attrReverse: "reverse", attrDim: "dim"}

func diffFrame(in *obj, want any) string {
	cur, _ := pyInt(in.val("cur"))
	h, _ := pyInt(in.val("h"))
	w, _ := pyInt(in.val("w"))
	got := []any{}
	for _, d := range frame(swapsFrom(in.val("swaps")), cur, h, w) {
		got = append(got, []any{int64(d.y), int64(d.x), d.text, attrNames[d.attr]})
	}
	return mismatch(in, got, want)
}

var keyNames = map[string]int{"down": keyDown, "up": keyUp, "left": keyLeft, "right": keyRight, "enter": '\n', "return": '\r',
	"m": 'm', "q": 'q', "escape": keyEscape, "j": 'j', "k": 'k', "h": 'h', "l": 'l', "x": 'x'}

func skillNameList(skills []skill) []any {
	out := []any{}
	for _, s := range skills {
		out = append(out, s.Name)
	}
	return out
}

func diffKeys(in *obj, want any) string {
	swaps := swapsFrom(in.val("swaps"))
	cur, _ := pyInt(in.val("cur"))
	done := false
	for _, name := range stringList(in.val("keys")) {
		if !done {
			swaps, cur, done = onKey(swaps, cur, keyNames[name])
		}
	}
	state, taken := []any{}, []any{}
	for _, s := range swaps {
		state = append(state, objOf("idx", int64(s.Idx), "ok", s.OK, "out", int64(s.Out), "mute", skillNameList(s.Mute), "held", skillNameList(s.Held)))
	}
	for _, s := range accepted(swaps) {
		taken = append(taken, s.cand().Name)
	}
	return mismatch(in, objOf("cur", int64(cur), "done", done, "state", state, "accepted", taken), want)
}

func diffLower(want *obj) string {
	var wrong []string
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		expect := string(r)
		if v, ok := want.str(itoa(int(r))); ok {
			expect = v
		}
		if pyLower(string(r)) != expect && len(wrong) < 10 {
			wrong = append(wrong, hexPad(r, 4))
		}
	}
	return strings.Join(wrong, " ")
}

func itoa(n int) string { return dumpJSON(int64(n)) }

func diffFold(want *obj) string {
	var wrong []string
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		expect, _ := want.str(itoa(int(r)))
		got := ""
		if f := foldRune(r); f >= 'a' && f <= 'z' {
			got = string(f)
		}
		if got != expect && len(wrong) < 10 {
			wrong = append(wrong, hexPad(r, 4)+"="+expect)
		}
	}
	return strings.Join(wrong, " ")
}

func diffJSON(text string, want *obj) string {
	v, err := parseJSON(text)
	if (err == nil) != (want.val("ok") == true) {
		return "parse ok mismatch for " + text
	}
	if err != nil {
		return ""
	}
	dumped := want.list("dumped")
	if dumped == nil {
		return ""
	}
	got := []any{dumpJSON(v), dumpJSONText(v), dumped[2]}
	if _, isString := v.(string); !isString {
		got[2] = pyStr(v)
	}
	return mismatch(text, got, dumped)
}

func diffDecimals(want *obj) string {
	var wrong []string
	for r := rune(0); r <= unicode.MaxRune; r++ {
		d, ok := decimalValue(r)
		expect, has := want.val(itoa(int(r))).(int64)
		if ok != has || (ok && int64(d) != expect) {
			wrong = append(wrong, hexPad(r, 4))
		}
	}
	return strings.Join(wrong[:min(10, len(wrong))], " ")
}

func promptByName(name string) string {
	return map[string]string{"group": groupPrompt, "assign": assignPrompt, "cover": coverPrompt, "verify": verifyPrompt,
		"write": writePrompt, "system": systemPrompt, "lean": strings.Join(leanFlags, "\x00")}[name]
}

func verdictAsJSON(v verdict) *obj {
	if !v.Keep {
		return objOf("keep", false, "reason", v.Reason)
	}
	return objOf("keep", true, "risk", v.Risk, "fit", int64(v.Fit), "reason", v.Reason, "use", v.Use)
}

func diffVerdict(in []any, want *obj) string {
	answer, doc := in[0].(string), in[1].(string)
	got := verdictAsJSON(checkedVerdict(verdictJSON(answer), doc))
	return mismatch(in, got, want)
}

func skillsFrom(v any) []skill {
	var out []skill
	list, _ := v.([]any)
	for _, raw := range list {
		o := raw.(*obj)
		s := skill{}
		s.Name, _ = o.str("name")
		s.Desc, _ = o.str("desc")
		s.Tokens, _ = pyInt(o.val("tokens"))
		out = append(out, s)
	}
	return out
}

func diffPair(in *obj, want any) string {
	var cost *int
	if n, ok := in.val("cost").(int64); ok {
		cost = intPtr(int(n))
	}
	var got []any
	for _, s := range pair(cost, skillsFrom(in.val("pool"))) {
		got = append(got, s.Name)
	}
	if got == nil {
		got = []any{}
	}
	return mismatch(in, got, want)
}

func diffVerifyKey(in *obj, want any) string {
	a := newTestApp()
	c := candidate{}
	c.Task, _ = in.str("task")
	c.Doc, _ = in.str("doc")
	c.Name, _ = in.str("name")
	c.Source, _ = in.str("source")
	c.Slug, _ = in.str("slug")
	c.ID, _ = in.str("id")
	c.Kind, _ = in.str("kind")
	c.Created = in.val("created") == true
	c.Server = in.object("server")
	req := requestFor(c, skillsFrom(in.val("used")), skillsFrom(in.val("installed")))
	return mismatch(in, []any{a.verifyKey(req), verifyBody(req, "f00d"), docHash(c.Doc)}, want)
}

func newTestApp() *app {
	return newApp(strings.NewReader(""), &strings.Builder{}, &strings.Builder{})
}

// TestSessionsAgainstPrototype compares the sessions the port reads from a
// folder of real transcripts with what the prototype read from the same folder
// (its own-folder rule aside: see `data/diff_cases.py sessions`).
func TestSessionsAgainstPrototype(t *testing.T) {
	dump, projects := os.Getenv("REINVEST_DIFF_SESSIONS"), os.Getenv("REINVEST_DIFF_PROJECTS")
	if dump == "" || projects == "" {
		t.Skip("set REINVEST_DIFF_SESSIONS, REINVEST_DIFF_SKILLS and REINVEST_DIFF_PROJECTS (see data/diff_cases.py)")
	}
	want := readJSONFile(dump).(*obj)
	names := map[string]bool{}
	for _, name := range stringList(readJSONFile(os.Getenv("REINVEST_DIFF_SKILLS"))) {
		names[name] = true
	}
	a := newTestApp()
	a.projectsDir = projects
	got := a.readSessions(120, names)
	expected := want.list("sessions")
	if len(got) != len(expected) {
		t.Errorf("%d sessions, the prototype read %d", len(got), len(expected))
	}
	weeks := want.object("weeks")
	differ := 0
	for i := 0; i < len(got) && i < len(expected); i++ {
		record := sessionRecord(got[i])
		full := make([]any, len(got[i].Prompts))
		for n, p := range got[i].Prompts {
			full[n] = p
		}
		record = record.with("prompts", full)
		for key, n := range map[string]int{"tools": got[i].Tools, "errors": got[i].Errors, "interrupts": got[i].Interrupts, "tokens": got[i].Tokens} {
			record = record.with(key, int64(n)) // as a number read from JSON, to compare with one
		}
		if !jsonEqual(record, expected[i]) {
			differ++
			if differ <= 3 {
				t.Errorf("session %d differs:\n   got  %.600s\n   want %.600s", i, dumpJSON(record), dumpJSON(expected[i]))
			}
		}
		if w, _ := weeks.str(got[i].ID); w != week(got[i].TS) {
			t.Errorf("week of %s: got %s, want %s", got[i].ID, week(got[i].TS), w)
		}
	}
	t.Logf("%d sessions compared, %d differ", min(len(got), len(expected)), differ)
}
