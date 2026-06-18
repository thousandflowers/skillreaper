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
