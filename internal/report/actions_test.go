package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// The DO line for prune counts what a bare `reap prune` takes: unused items it
// can remove. An unused link or plugin item is in the dead count, not in prune.
func TestTheDoLineForPruneCountsWhatPruneTakes(t *testing.T) {
	row := func(name string, removable bool) Row {
		return Row{Item: scan.Item{Category: scan.CatSkill, Name: name, Removable: removable}, Verdict: VerdictReap, Tokens: 19}
	}
	r := &Report{Rows: []Row{row("real", true), row("linked", false)}, DeadCount: 2, DeadTokensPerSession: 38}
	var b bytes.Buffer

	renderActions(&b, r, 100, func(_, s string) string { return s })

	if out := b.String(); !strings.Contains(out, "reap prune   1 item · ~19 tok/session reclaimed") {
		t.Errorf("the DO block reads:\n%s", out)
	}
}

func TestSourceIsTakesAnIdOrAOneSourceLabel(t *testing.T) {
	for _, c := range []struct {
		src, want string
		is        bool
	}{{"personal", "personal", true}, {"personal", "own", true}, {"user-config", "usr", true},
		{"plugin:a@b", "ext", false}, {"plugin:a@b", "plugin:a@b", true}, {"personal", "usr", false}} {
		if got := SourceIs(c.src, c.want); got != c.is {
			t.Errorf("SourceIs(%q, %q) = %v", c.src, c.want, got)
		}
	}
}

// The --agent footer says items were held back only when some were; a
// warning alone (a retention advisory, say) is counted, not explained away.
func TestTheAgentFooterSaysOnlyWhatHappened(t *testing.T) {
	advisory := &Report{Warnings: []scan.Warning{{Msg: "oldest transcript is 28 days old", Advisory: true}}}
	held := &Report{Warnings: advisory.Warnings, Rows: []Row{{Item: scan.Item{Category: scan.CatSkill, Name: "x"}, Verdict: VerdictReview, Reason: ReasonNoEvidence}}}
	var a, b bytes.Buffer
	RenderAgent(&a, advisory, 10)
	RenderAgent(&b, held, 10)
	if strings.Contains(a.String(), "held back") || !strings.Contains(a.String(), "1 warning — run reap for details") {
		t.Errorf("advisory only:\n%s", a.String())
	}
	if !strings.Contains(b.String(), "1 item held back from a REAP or MUTE verdict: its usage could not be read") {
		t.Errorf("one held back:\n%s", b.String())
	}
}
