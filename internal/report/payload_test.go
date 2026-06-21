package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/usage"
)

func TestComputePayloadClassifiesAndSorts(t *testing.T) {
	st := usage.NewStats(30)
	st.MCPPayload = map[string]usage.PayloadStat{
		// fires a lot, mostly noise → Noisy
		"mcp__fetch__fetch": {Calls: 40, TotalChars: 1000, NoiseChars: 850},
		// fires a lot, clean → not noisy
		"mcp__github__search_code": {Calls: 30, TotalChars: 1000, NoiseChars: 100},
		// noisy ratio but too few calls → not noisy
		"mcp__rare__thing": {Calls: 2, TotalChars: 1000, NoiseChars: 900},
		// no measured bytes → skipped entirely
		"mcp__empty__tool": {Calls: 5, TotalChars: 0, NoiseChars: 0},
	}

	rows := computePayload(st)
	if len(rows) != 3 {
		t.Fatalf("want 3 scored rows (empty skipped), got %d", len(rows))
	}
	// Worst quality first.
	if rows[0].Tool != "mcp__rare__thing" {
		t.Errorf("worst-quality first: got %s", rows[0].Tool)
	}
	byTool := map[string]PayloadRow{}
	for _, r := range rows {
		byTool[r.Tool] = r
	}
	if !byTool["mcp__fetch__fetch"].Noisy {
		t.Error("fetch fires often + mostly noise → should be Noisy")
	}
	if byTool["mcp__github__search_code"].Noisy {
		t.Error("clean high-firing tool → must not be Noisy")
	}
	if byTool["mcp__rare__thing"].Noisy {
		t.Error("rare tool below min-calls → must not be Noisy")
	}
	if got := byTool["mcp__fetch__fetch"].QualityPct; got != 15 {
		t.Errorf("quality pct: want 15, got %d", got)
	}
	if got := byTool["mcp__fetch__fetch"].Server; got != "fetch" {
		t.Errorf("server segment: want fetch, got %s", got)
	}
}

func TestComputePayloadNilSafe(t *testing.T) {
	if got := computePayload(nil); got != nil {
		t.Errorf("nil stats: want nil, got %v", got)
	}
	if got := computePayload(usage.NewStats(30)); got != nil {
		t.Errorf("empty payload: want nil, got %v", got)
	}
}

func TestRenderGapJSONIncludesPayload(t *testing.T) {
	r := &Report{
		Gap: &Gap{},
		MCPPayload: []PayloadRow{
			{Tool: "mcp__fetch__fetch", Server: "fetch", Calls: 40, TotalChars: 1000, NoiseChars: 850, QualityPct: 15, Noisy: true},
		},
	}
	var buf bytes.Buffer
	if err := RenderGapJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if _, ok := out["payload"]; !ok {
		t.Error("gap json must carry a payload key")
	}
	if !strings.Contains(buf.String(), "mcp__fetch__fetch") {
		t.Error("payload json must name the tool")
	}
}
