package report

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderDeadToolCharsShownPerSession(t *testing.T) {
	// DeadToolChars is a total summed across all sessions; the line labels it
	// "per session", so it must be divided by the session count before display.
	r := &Report{
		Sessions:      2,
		DeadCount:     1,
		DeadToolChars: 40, // 20 per session across 2 sessions
	}
	var buf bytes.Buffer
	RenderText(&buf, r, false)
	out := buf.String()

	if !strings.Contains(out, "~20 chars of tool descriptions unused per session") {
		t.Errorf("expected per-session figure of 20, got:\n%s", out)
	}
	if strings.Contains(out, "~40 chars") {
		t.Errorf("rendered the cross-session total (40) as a per-session figure:\n%s", out)
	}
}

func TestRenderDeadToolCharsSingleSession(t *testing.T) {
	// With one session the per-session figure equals the total.
	r := &Report{Sessions: 1, DeadCount: 1, DeadToolChars: 33}
	var buf bytes.Buffer
	RenderText(&buf, r, false)
	if !strings.Contains(buf.String(), "~33 chars of tool descriptions unused per session") {
		t.Errorf("expected 33 with a single session, got:\n%s", buf.String())
	}
}

func TestTruncatePreservesWholeRunes(t *testing.T) {
	// A multibyte name must be truncated on a rune boundary, not a byte
	// boundary, so the terminal never receives invalid UTF-8.
	in := "日本語のスキル名" // each rune is 3 bytes
	got := truncate(in, 5)
	if !utf8.ValidString(got) {
		t.Errorf("truncate produced invalid UTF-8: %q", got)
	}
	// 4 runes + ellipsis.
	if rc := utf8.RuneCountInString(got); rc != 5 {
		t.Errorf("truncate rune count = %d, want 5 (%q)", rc, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate should end with ellipsis, got %q", got)
	}
}

func TestTruncateShortStringUnchanged(t *testing.T) {
	for _, s := range []string{"short", "日本", ""} {
		if got := truncate(s, 44); got != s {
			t.Errorf("truncate(%q, 44) = %q, want unchanged", s, got)
		}
	}
}

// Audit M3. "2 unreadable lines" is true of a corrupt file, a truncated one,
// and a healthy file holding one oversized record. A reader deciding whether
// to trust the verdicts needs to tell those apart.
func TestMalformedSummary(t *testing.T) {
	tests := []struct {
		name string
		r    Report
		want string
	}{
		{"single kind", Report{MalformedLines: 1, UnreadableFiles: 1}, "1 unreadable file"},
		{"plural", Report{MalformedLines: 3, OversizedLines: 3}, "3 oversized records"},
		{
			"several kinds",
			Report{MalformedLines: 4, UnreadableFiles: 1, TruncatedReads: 1, OversizedLines: 2},
			"1 unreadable file, 1 truncated read, 2 oversized records",
		},
		// Merged stats from a source that never recorded a breakdown still
		// have a total, and it must read sensibly rather than as an empty list.
		{"no breakdown falls back to the total", Report{MalformedLines: 7}, "7 unreadable lines"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MalformedSummary(&tt.r); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHumanBigScalesPastThousands(t *testing.T) {
	for _, c := range []struct {
		n    int64
		want string
	}{{999, "999"}, {1500, "1.5k"}, {2_500_000, "2.5M"}, {3_738_237_538, "3.7B"}} {
		if got := humanBig(c.n); got != c.want {
			t.Errorf("humanBig(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// The privacy claim is the one line in the report that exists for a reader who
// will never open the repository page, so it is pinned here rather than left to
// whoever next edits the closing block. The machine formats must not carry it:
// --agent output is pasted verbatim into a conversation and --json is parsed.
func TestTextReportStatesTheAnalysisWasLocal(t *testing.T) {
	r := &Report{Sessions: 1}

	// Collapsed, because the line wraps to the terminal width and the claim
	// would otherwise be asserted only at the widths where it happens to fit.
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	const claim = "Analysis is local: only files on this machine were read, nothing was sent over the network."

	var text bytes.Buffer
	RenderText(&text, r, false)
	if !strings.Contains(flat(text.String()), claim) {
		t.Errorf("the text report must say the analysis was local, got:\n%s", text.String())
	}

	var agent bytes.Buffer
	RenderAgent(&agent, r, 10)
	if strings.Contains(flat(agent.String()), claim) {
		t.Error("the agent format must stay clean of the privacy line")
	}
}
