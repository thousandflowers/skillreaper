package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// deadRow builds a REAP row with the given name and token weight.
func deadRow(name string, tokens int) Row {
	return Row{
		Item:    scan.Item{Category: scan.CatSkill, Name: name},
		Verdict: VerdictReap,
		Reason:  ReasonUnused,
		Tokens:  tokens,
	}
}

// agentFixture is the baseline report the byte-for-byte tests render. It is
// built by hand rather than through Build so the expected output cannot drift
// when verdict thresholds change.
func agentFixture() *Report {
	return &Report{
		WindowDays:           30,
		Sessions:             12,
		DeadCount:            2,
		DeadTokensPerSession: 445,
		MoneyPerMonth:        1.5,
		Rows: []Row{
			deadRow("acme:legacy-schema-migration", 300),
			deadRow("demo:unused-helper", 145),
			{Item: scan.Item{Category: scan.CatSkill, Name: "demo:used"}, Verdict: VerdictKeep, Reason: ReasonUsed, Tokens: 50},
		},
		Gap: &Gap{
			PerCat: []GapCat{
				{Category: scan.CatSkill, Loaded: 3, Fired: 1, LoadedTok: 495, FiredTok: 50},
				{Category: scan.CatMCP, Loaded: 2, Fired: 1, LoadedTok: 0, FiredTok: 0},
			},
			Loaded: 5, Fired: 2, LoadedTok: 495, FiredTok: 50,
		},
	}
}

func renderAgentString(r *Report) string {
	var b bytes.Buffer
	RenderAgent(&b, r)
	return b.String()
}

func renderGapAgentString(r *Report) string {
	var b bytes.Buffer
	RenderGapAgent(&b, r)
	return b.String()
}

func TestRenderAgentExactBytes(t *testing.T) {
	want := `skillreaper · last 30d · 12 sessions
2/5 items fired · 40% utilization
2 never used · ~445 dead tokens/session · ~$1.50/month

TOKENS  CATEGORY  NAME                          VERDICT  REASON
300     skill     acme:legacy-schema-migration  REAP     unused
145     skill     demo:unused-helper            REAP     unused

To prune: reap prune   (interactive, reversible via reap restore --all)

measured by skillreaper · github.com/thousandflowers/skillreaper
`
	if got := renderAgentString(agentFixture()); got != want {
		t.Errorf("RenderAgent output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderGapAgentExactBytes(t *testing.T) {
	// The mcp row carries LoadedTok 0 on purpose: it is the zero-denominator
	// case, and REACH must read n/a rather than 0%.
	want := `skillreaper · loaded vs fired
2/5 items fired · 40% utilization
50/495 tokens touched · 10% token reach

CATEGORY  LOADED  FIRED  UTIL  LOADED TOK  TOUCHED TOK  REACH
skill     3       1      33%   495         50           10%
mcp       2       1      50%   0           0            n/a

REACH n/a means there are no loaded tokens to divide by: that weight lives
in tool schemas rather than in an injected description, so it is not counted.

measured by skillreaper · github.com/thousandflowers/skillreaper
`
	if got := renderGapAgentString(agentFixture()); got != want {
		t.Errorf("RenderGapAgent output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderAgentCapsRowsAtAgentMaxRows(t *testing.T) {
	r := agentFixture()
	r.Rows = nil
	const total = AgentMaxRows + 7
	for i := 0; i < total; i++ {
		// Descending weights so the order is unambiguous.
		r.Rows = append(r.Rows, deadRow(fmt.Sprintf("demo:skill-%02d", i), 1000-i))
	}
	r.DeadCount = total

	got := renderAgentString(r)

	if n := strings.Count(got, "REAP"); n != AgentMaxRows {
		t.Errorf("rendered %d REAP rows, want AgentMaxRows (%d)", n, AgentMaxRows)
	}
	wantMore := fmt.Sprintf("(%d more never-used items not shown — use --json for all)", total-AgentMaxRows)
	if !strings.Contains(got, wantMore) {
		t.Errorf("missing overflow line %q in:\n%s", wantMore, got)
	}
	// The 11th-heaviest item must not appear.
	if strings.Contains(got, "demo:skill-10") {
		t.Error("row beyond the cap was rendered")
	}
}

func TestRenderAgentNoOverflowLineAtExactlyMaxRows(t *testing.T) {
	r := agentFixture()
	r.Rows = nil
	for i := 0; i < AgentMaxRows; i++ {
		r.Rows = append(r.Rows, deadRow(fmt.Sprintf("demo:skill-%02d", i), 1000-i))
	}
	r.DeadCount = AgentMaxRows

	if got := renderAgentString(r); strings.Contains(got, "not shown") {
		t.Errorf("overflow line rendered at exactly the cap:\n%s", got)
	}
}

func TestRenderAgentNoDeadRows(t *testing.T) {
	r := agentFixture()
	r.Rows = []Row{{Item: scan.Item{Category: scan.CatSkill, Name: "demo:used"}, Verdict: VerdictKeep, Reason: ReasonUsed, Tokens: 50}}
	r.DeadCount = 0
	r.DeadTokensPerSession = 0
	r.MoneyPerMonth = 0

	want := `skillreaper · last 30d · 12 sessions
2/5 items fired · 40% utilization
0 never used · ~0 dead tokens/session · ~$0.00/month

Nothing unused in this window.

measured by skillreaper · github.com/thousandflowers/skillreaper
`
	got := renderAgentString(r)
	if got != want {
		t.Errorf("empty-report output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if strings.Contains(got, "TOKENS") {
		t.Error("table header rendered with no dead rows")
	}
	if strings.Contains(got, "reap prune") {
		t.Error("prune hint rendered with nothing to prune")
	}
}

func TestRenderAgentNeverTruncatesNames(t *testing.T) {
	// 44 is the width RenderText clips at; this name is deliberately longer so
	// a borrowed truncate() would show up.
	const long = "acme:a-deliberately-very-long-skill-name-that-exceeds-any-sane-column-width"
	r := agentFixture()
	r.Rows = []Row{deadRow(long, 300)}
	r.DeadCount = 1

	got := renderAgentString(r)
	if !strings.Contains(got, long) {
		t.Errorf("name was altered; want the full %q in:\n%s", long, got)
	}
	if strings.Contains(got, "…") {
		t.Error("output contains an ellipsis — a name was truncated")
	}
}

func TestRenderAgentNoANSI(t *testing.T) {
	cases := map[string]func(*Report) string{
		"RenderAgent":    renderAgentString,
		"RenderGapAgent": renderGapAgentString,
	}
	for name, render := range cases {
		t.Run(name, func(t *testing.T) {
			if got := render(agentFixture()); strings.ContainsRune(got, '\x1b') {
				t.Errorf("%s emitted an ANSI escape:\n%q", name, got)
			}
		})
	}
}

func TestRenderAgentIsDeterministic(t *testing.T) {
	// Six renders, the same check G5 runs against the real binary.
	first := renderAgentString(agentFixture())
	firstGap := renderGapAgentString(agentFixture())
	for i := 0; i < 5; i++ {
		if got := renderAgentString(agentFixture()); got != first {
			t.Fatalf("RenderAgent run %d differs from run 0", i+1)
		}
		if got := renderGapAgentString(agentFixture()); got != firstGap {
			t.Fatalf("RenderGapAgent run %d differs from run 0", i+1)
		}
	}
}

func TestRenderAgentNoItemsLoaded(t *testing.T) {
	r := agentFixture()
	r.Gap = nil
	r.Rows = nil
	r.DeadCount = 0
	r.DeadTokensPerSession = 0
	r.MoneyPerMonth = 0

	got := renderAgentString(r)
	if !strings.Contains(got, "no items loaded") {
		t.Errorf("want the no-items line rather than a division by zero:\n%s", got)
	}
	if strings.Contains(got, "utilization") {
		t.Errorf("utilization was computed with no denominator:\n%s", got)
	}

	gapOut := renderGapAgentString(r)
	if !strings.Contains(gapOut, "no items loaded") {
		t.Errorf("RenderGapAgent: want the no-items line:\n%s", gapOut)
	}
	if !strings.HasSuffix(gapOut, agentSignature+"\n") {
		t.Errorf("RenderGapAgent: early return dropped the signature:\n%s", gapOut)
	}
}

func TestPctOf(t *testing.T) {
	cases := []struct {
		part, whole int
		want        string
	}{
		{0, 0, "n/a"},  // no denominator at all
		{5, 0, "n/a"},  // fired with no measured weight (MCP)
		{0, 100, "0%"}, // a real zero
		{1, 1000, "<1%"},
		{7, 378, "1%"}, // matches utilPct: the tool truncates, never rounds
		{50, 100, "50%"},
	}
	for _, c := range cases {
		if got := pctOf(c.part, c.whole); got != c.want {
			t.Errorf("pctOf(%d, %d) = %q, want %q", c.part, c.whole, got, c.want)
		}
	}
}
