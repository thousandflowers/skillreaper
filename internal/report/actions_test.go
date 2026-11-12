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
