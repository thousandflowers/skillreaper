package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllReturnsAll(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("All() returned empty")
	}
	seen := map[ID]bool{}
	for _, p := range all {
		if seen[p.ID] {
			t.Errorf("duplicate platform ID %s", p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" {
			t.Errorf("platform %s has empty Name", p.ID)
		}
	}
}

func TestDetectOnMachine(t *testing.T) {
	installed := Detect()
	for _, p := range installed {
		if p.ConfigDirAbs == "" {
			t.Errorf("Detect returned %s with empty ConfigDirAbs", p.ID)
		}
		info, err := os.Stat(p.ConfigDirAbs)
		if err != nil {
			t.Errorf("Detect returned %s but ConfigDirAbs %s is not stattable: %v", p.ID, p.ConfigDirAbs, err)
		}
		if !info.IsDir() {
			t.Errorf("Detect returned %s but ConfigDirAbs %s is not a directory", p.ID, p.ConfigDirAbs)
		}
	}
}

func TestDetectFindsClaudeCode(t *testing.T) {
	installed := Detect()
	found := false
	for _, p := range installed {
		if p.ID == ClaudeCode {
			found = true
			break
		}
	}
	if !found {
		t.Log("Claude Code not installed on this machine (may be expected in CI)")
	}
}

func TestDetectEmptyOnFakeHome(t *testing.T) {
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	tmp := t.TempDir()
	os.Setenv("HOME", tmp)

	detected := Detect()
	for _, p := range detected {
		t.Errorf("expected no platforms in fake home, got %s at %s", p.ID, p.ConfigDirAbs)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		input string
		want  string
	}{
		{"~", home},
		{"~/foo", filepath.Join(home, "foo")},
		{"/abs/path", "/abs/path"},
		{"", ""},
	}
	for _, tc := range tests {
		got := expandHome(tc.input)
		if got != tc.want {
			t.Errorf("expandHome(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveReturnsEmptyOnMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := All()[0]
	resolved := resolve(p)
	if resolved.ConfigDirAbs != "" {
		t.Errorf("expected empty ConfigDirAbs for missing dir, got %q", resolved.ConfigDirAbs)
	}
}

func TestDirExists(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "sub")
	if dirExists(sub) {
		t.Error("expected false for non-existent dir")
	}
	os.MkdirAll(sub, 0o755)
	if !dirExists(sub) {
		t.Error("expected true for existing dir")
	}
}
