package usage

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNoiseChars(t *testing.T) {
	clean := "This is an ordinary sentence with real, useful content in it."
	if got := noiseChars(clean); got != 0 {
		t.Errorf("clean prose: want 0 noise, got %d", got)
	}

	blob := strings.Repeat("A", 200)
	if got := noiseChars(blob); got != len(blob) {
		t.Errorf("base64 blob: want %d noise, got %d", len(blob), got)
	}

	short := strings.Repeat("A", 40) // below payloadNoiseMinRun
	if got := noiseChars(short); got != 0 {
		t.Errorf("short token: want 0 noise, got %d", got)
	}

	html := "<div class=\"nav\"><span>x</span></div>"
	if got := noiseChars(html); got == 0 || got > len(html) {
		t.Errorf("html soup: want some noise <= %d, got %d", len(html), got)
	}
}

func TestNoiseCharsRepeatedLines(t *testing.T) {
	footer := "© 2026 Example Corp — all rights reserved"
	s := "real content line\n" + footer + "\n" + footer + "\n" + footer
	// The first footer occurrence is signal; the two repeats are noise.
	want := 2 * len(footer)
	if got := noiseChars(s); got != want {
		t.Errorf("repeated footer: want %d noise, got %d", want, got)
	}
}

func TestNoiseCharsNoDoubleCount(t *testing.T) {
	// A data URI also matches the bare base64 rule; the mask must count it once.
	uri := "data:image/png;base64," + strings.Repeat("Q", 120)
	if got := noiseChars(uri); got != len(uri) {
		t.Errorf("data uri: want %d (counted once), got %d", len(uri), got)
	}
}

func TestClassifyPayloadString(t *testing.T) {
	raw, _ := json.Marshal("plain useful text result")
	total, noise := classifyPayload(raw)
	if total != len("plain useful text result") {
		t.Errorf("total: want %d, got %d", len("plain useful text result"), total)
	}
	if noise != 0 {
		t.Errorf("noise: want 0, got %d", noise)
	}
}

func TestClassifyPayloadImageBlock(t *testing.T) {
	blocks := []map[string]any{
		{"type": "text", "text": "ok"},
		{"type": "image", "source": map[string]any{"data": strings.Repeat("Z", 500)}},
	}
	raw, _ := json.Marshal(blocks)
	total, noise := classifyPayload(raw)
	if total != 2+500 {
		t.Errorf("total: want %d, got %d", 502, total)
	}
	// Inline base64 image bytes are fully noise; the "ok" text is signal.
	if noise != 500 {
		t.Errorf("noise: want 500, got %d", noise)
	}
}

func TestClassifyPayloadEmptyAndGarbage(t *testing.T) {
	for _, in := range []string{"", "   ", "null"} {
		total, noise := classifyPayload(json.RawMessage(in))
		if total != 0 || noise != 0 {
			t.Errorf("input %q: want 0/0, got %d/%d", in, total, noise)
		}
	}
}

func TestPayloadParts(t *testing.T) {
	raw, _ := json.Marshal("hello")
	text, b64 := payloadParts(raw)
	if text != "hello" || b64 != 0 {
		t.Errorf("string: got %q/%d", text, b64)
	}
}

// TestRecordPayloadAccumulates is the self-check that the parser-facing
// accumulator sums calls and bytes per tool key.
func TestRecordPayloadAccumulates(t *testing.T) {
	st := NewStats(30)
	st.recordPayload("mcp__srv__fetch", 100, 80)
	st.recordPayload("mcp__srv__fetch", 50, 10)
	st.recordPayload("", 999, 999) // empty key is ignored

	p := st.MCPPayload["mcp__srv__fetch"]
	if p.Calls != 2 || p.TotalChars != 150 || p.NoiseChars != 90 {
		t.Errorf("accumulate: got %+v", p)
	}
	if len(st.MCPPayload) != 1 {
		t.Errorf("empty key must not create an entry: %d entries", len(st.MCPPayload))
	}
}
