package report

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The plain report points at reinvest's last proposals with one line, using
// the command reinvest itself recorded; with nothing to point at it is silent.
func TestRenderReinvestHint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "swaps.json")
	for name, c := range map[string]struct{ file, want string }{
		"two swaps": {`{"at":"2026-10-02T10:00:00Z","command":"python3 reinvest.py","swaps":[{},{}]}`,
			"  reinvest · 2 swaps ready (from 2026-10-02)\n           run `python3 reinvest.py` to review them\n"},
		"one swap":        {`{"at":"2026-10-02T10:00:00Z","command":"x","swaps":[{}]}`, "1 swap ready"},
		"none":            {`{"at":"2026-10-02T10:00:00Z","command":"x","swaps":[]}`, ""},
		"no command":      {`{"at":"2026-10-02T10:00:00Z","swaps":[{}]}`, ""},
		"malformed":       {`{`, ""},
		"escape stripped": {"{\"at\":\"2026-10-02T10:00:00Z\",\"command\":\"a\\u001b[31mb\",\"swaps\":[{}]}", "run `a[31mb`"},
	} {
		if err := os.WriteFile(path, []byte(c.file), 0o644); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		RenderReinvestHint(&out, path, false)
		if c.want == "" && out.Len() != 0 || c.want != "" && !strings.Contains(out.String(), c.want) {
			t.Errorf("%s: got %q, want %q", name, out.String(), c.want)
		}
	}
	var out bytes.Buffer
	RenderReinvestHint(&out, filepath.Join(dir, "missing.json"), false)
	if out.Len() != 0 {
		t.Errorf("missing file: got %q, want nothing", out.String())
	}
}
