package scan

import (
	"os"
	"path/filepath"
	"testing"
)

// Issue #64. The platform table named a TOML file and a JSONC file while the
// only reader was encoding/json, so both failed by construction and neither
// platform's servers were ever inventoried.
func TestScanMCPReadsDeclaredFormats(t *testing.T) {
	dir := t.TempDir()

	toml := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(toml, []byte(`
# Codex config
[tui]
theme = "dark"

[mcp_servers.filesystem]
command = "npx"

[mcp_servers.fetch]
command = "uvx"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	jsonc := filepath.Join(dir, "opencode.jsonc")
	if err := os.WriteFile(jsonc, []byte(`{
  // the schema URL is the case a naive stripper breaks
  "$schema": "https://opencode.ai/config.json",
  "mcpServers": {
    "weather": { "command": "node" },
  },
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("toml", func(t *testing.T) {
		items, warns := ScanMCP(toml, dir, "codex", "toml")
		if len(warns) != 0 {
			t.Errorf("warnings on a valid TOML config: %v", warns)
		}
		names := map[string]bool{}
		for _, i := range items {
			names[i.Name] = true
		}
		for _, want := range []string{"filesystem", "fetch"} {
			if !names[want] {
				t.Errorf("server %q was not inventoried; got %v", want, names)
			}
		}
	})

	t.Run("jsonc", func(t *testing.T) {
		items, warns := ScanMCP(jsonc, dir, "opencode", "jsonc")
		if len(warns) != 0 {
			t.Errorf("warnings on a valid JSONC config: %v", warns)
		}
		found := false
		for _, i := range items {
			if i.Name == "weather" {
				found = true
			}
		}
		if !found {
			t.Error("the JSONC config's server was not inventoried")
		}
	})

	// A malformed file must still warn: the point is to read the formats the
	// table declares, not to stop reporting real breakage.
	t.Run("still warns on genuinely broken input", func(t *testing.T) {
		broken := filepath.Join(dir, "broken.jsonc")
		if err := os.WriteFile(broken, []byte(`{"a": `), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, warns := ScanMCP(broken, dir, "opencode", "jsonc"); len(warns) == 0 {
			t.Error("a truncated config produced no warning")
		}
	})
}
