package scan

import (
	"path/filepath"
	"testing"
)

func TestScanMCP(t *testing.T) {
	home := buildFixtureHome(t)
	plugDir := filepath.Join(home, "plugins", "cache", "mkt", "coolplug", "1.0.0")
	mustWrite(t, filepath.Join(plugDir, ".mcp.json"),
		`{"mcpServers":{"plugtool":{"command":"npx","args":["plugtool-mcp"]}}}`)

	claudeJSON := filepath.Join(home, "dotclaude.json")
	mustWrite(t, claudeJSON, `{
		"someOtherKey": 42,
		"mcpServers": {"globalsrv": {"command": "uvx", "args": ["globalsrv"]}},
		"projects": {
			"/Users/test/proj": {"mcpServers": {"projsrv": {"type": "http", "url": "http://localhost:9999"}}}
		}
	}`)

	items, warns := ScanMCP(claudeJSON, home)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3: %+v", len(items), items)
	}

	g := findItem(items, "globalsrv")
	if g == nil || g.Source != "user-config" || !g.Removable {
		t.Errorf("globalsrv wrong: %+v", g)
	}
	if g != nil && g.Description != "uvx globalsrv" {
		t.Errorf("globalsrv description = %q", g.Description)
	}

	p := findItem(items, "projsrv")
	if p == nil || p.Source != "project:/Users/test/proj" || !p.Removable {
		t.Errorf("projsrv wrong: %+v", p)
	}
	if p != nil && p.Description != "http://localhost:9999" {
		t.Errorf("projsrv description = %q", p.Description)
	}

	pl := findItem(items, "plugtool")
	if pl == nil || pl.Source != "plugin:coolplug@mkt" || pl.Removable {
		t.Errorf("plugtool wrong: %+v", pl)
	}
}

func TestScanMCPMissingFile(t *testing.T) {
	home := t.TempDir()
	items, warns := ScanMCP(filepath.Join(home, "nope.json"), home)
	if len(items) != 0 || len(warns) != 0 {
		t.Errorf("expected empty, got %d items %d warns", len(items), len(warns))
	}
}

func TestScanMCPCorrupt(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "bad.json")
	mustWrite(t, path, "{nope")
	_, warns := ScanMCP(path, home)
	if len(warns) != 1 {
		t.Errorf("expected 1 warning, got %d", len(warns))
	}
}
