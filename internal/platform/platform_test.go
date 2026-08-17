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

func TestGeminiCLIDefinition(t *testing.T) {
	all := All()
	var g *Info
	for i := range all {
		if all[i].ID == GeminiCLI {
			g = &all[i]
			break
		}
	}
	if g == nil {
		t.Fatal("Gemini CLI missing from All()")
	}
	if g.Name != "Gemini CLI" {
		t.Errorf("Name = %q", g.Name)
	}
	if !g.HasMCP || !g.HasProse {
		t.Errorf("Gemini exposes mcpServers in settings.json and GEMINI.md: HasMCP=%v HasProse=%v", g.HasMCP, g.HasProse)
	}
	if g.HasSkills || g.HasAgents || g.HasHooks {
		t.Errorf("Gemini has no SKILL.md skills, subagents or hooks: %+v", g)
	}
	// The safety property this platform depends on: it declares transcripts in
	// a format no parser reads, which is what makes cmdReport mark it
	// evidence-blind. Flipping HasTranscripts to false would silently turn
	// every Gemini item into a REAP verdict backed by no evidence at all.
	if !g.HasTranscripts {
		t.Error("HasTranscripts must stay true or Gemini items get REAP'd on evidence that was never collected")
	}
	if g.TranscriptType == "jsonl" || g.TranscriptType == "sqlite" {
		t.Errorf("TranscriptType %q claims a parser that does not read Gemini's layout", g.TranscriptType)
	}
}

func TestResolveGeminiCLI(t *testing.T) {
	origHome := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	home := t.TempDir()
	os.Setenv("HOME", home)

	gdir := filepath.Join(home, ".gemini")
	if err := os.MkdirAll(filepath.Join(gdir, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "settings.json"),
		[]byte(`{"mcpServers":{"srv":{"command":"npx","args":["srv-mcp"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gdir, "GEMINI.md"), []byte("context"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got *Info
	for _, p := range Detect() {
		if p.ID == GeminiCLI {
			c := p
			got = &c
			break
		}
	}
	if got == nil {
		t.Fatal("Detect did not find the Gemini install")
	}
	if got.ConfigDirAbs != gdir {
		t.Errorf("ConfigDirAbs = %q, want %q", got.ConfigDirAbs, gdir)
	}
	if want := filepath.Join(gdir, "settings.json"); got.ConfigFileAbs != want {
		t.Errorf("ConfigFileAbs = %q, want %q", got.ConfigFileAbs, want)
	}
	if want := filepath.Join(gdir, "GEMINI.md"); len(got.ProseDirs) != 1 || got.ProseDirs[0] != want {
		t.Errorf("ProseDirs = %v, want [%s]", got.ProseDirs, want)
	}
	if want := filepath.Join(gdir, "tmp"); len(got.TranscriptDirs) != 1 || got.TranscriptDirs[0] != want {
		t.Errorf("TranscriptDirs = %v, want [%s]", got.TranscriptDirs, want)
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

func TestAllProperNames(t *testing.T) {
	all := All()
	for _, p := range all {
		if p.ID == "" || p.Name == "" {
			t.Errorf("platform with empty ID or Name: %+v", p)
		}
	}
}

func TestExpandHomeNoChange(t *testing.T) {
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("expandHome('/abs/path') = %q", got)
	}
}

func TestDirExistsOnFile(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "afile")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirExists(f) {
		t.Error("dirExists should return false for a file")
	}
}
