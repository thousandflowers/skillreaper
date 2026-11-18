package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --claude-dir alone finds the config file where Claude Code keeps it beside
// that folder: inside it (CLAUDE_CONFIG_DIR's layout) or next to it (~/.claude
// and ~/.claude.json). It used to look nowhere, and every MCP server was gone.
func TestClaudeDirAloneFindsItsConfigFile(t *testing.T) {
	for _, inside := range []bool{false, true} {
		root := t.TempDir()
		dir := filepath.Join(root, ".claude")
		if err := os.MkdirAll(filepath.Join(dir, "projects"), 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := filepath.Join(root, ".claude.json")
		if inside {
			cfg = filepath.Join(dir, ".claude.json")
		}
		if err := os.WriteFile(cfg, []byte(`{"mcpServers":{"foo":{"command":"x"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out := reapRun("--claude-dir", dir, "--json")
		if code != 0 || !strings.Contains(out, `"Name": "foo"`) {
			t.Errorf("config inside %v: %d\n%s", inside, code, firstN(out, 400))
		}
	}
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
