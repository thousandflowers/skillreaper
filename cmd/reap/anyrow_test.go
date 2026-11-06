package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/report"
)

// anyRowHome is deleteHome with the rest of a real plugin: an agent, servers in
// both places a plugin can declare them, a skill that is a link.
func anyRowHome(t *testing.T) string {
	t.Helper()
	home := deleteHome(t)
	put := func(rel, body string) {
		t.Helper()
		p := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	base := ".claude/plugins/cache/m/p/1/"
	put(base+"agents/pa.md", "---\nname: pa\ndescription: plugin agent words\n---\nbody\n")
	put(base+"skills/pl/extra/notes.txt", "keep these bytes\n")
	put(base+".mcp.json", "{\n  \"mcpServers\": {\n    \"psrv\": {\"command\": \"a\"},\n    \"pother\": {\"command\": \"b\"}\n  }\n}\n")
	put(base+".claude-plugin/plugin.json", "{\"name\":\"p\",\"version\":\"1\",\"mcpServers\":{\"pinline\":{\"command\":\"c\"}}}\n")
	put("dot/plink/SKILL.md", "---\nname: plink\ndescription: linked words\n---\nbody\n")
	if err := os.Symlink(filepath.Join(home, "dot/plink"), filepath.Join(home, base, "skills/plink")); err != nil {
		t.Skip(err)
	}
	return home
}

// reapRun runs the CLI and returns exit code and everything it printed.
func reapRun(args ...string) (int, string) {
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(""), &out, &errOut)
	return code, out.String() + errOut.String()
}

// tree is every path under dir with its bytes, or its link text. Quarantine and
// the sessions are left out: they are what a prune is supposed to change.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() && (rel == ".claude/reaped" || rel == "Library" || rel == ".cache") {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			out[rel] = "link:" + target
		case info.Mode().IsRegular():
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out[rel] = fmt.Sprintf("%x %v", sha256.Sum256(b), info.Mode())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, want, got map[string]string) {
	t.Helper()
	for p, v := range want {
		if got[p] != v {
			t.Errorf("%s changed: %q -> %q", p, v, got[p])
		}
	}
	for p := range got {
		if _, ok := want[p]; !ok {
			t.Errorf("%s appeared", p)
		}
	}
}

func lexist(p string) bool { _, err := os.Lstat(p); return err == nil }

func reportRows(t *testing.T) []report.Row {
	t.Helper()
	code, out := reapRun("--json")
	if code != 0 {
		t.Fatalf("report: %d %s", code, out)
	}
	var r struct{ Rows []report.Row }
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("report json: %v\n%s", err, out)
	}
	return r.Rows
}

// The main safety line: a bulk run takes what it always took, whatever is on disk.
func TestBulkPruneNeverTakesPluginItemsLinksOrSharedFolders(t *testing.T) {
	for _, args := range [][]string{
		{"prune", "--yes", "--min-sessions", "1"},
		{"prune", "--yes", "--min-sessions", "1", "--source", "plugin:p@m"},
		{"prune", "--yes", "--min-sessions", "1", "--source", "personal"},
	} {
		t.Run(strings.Join(args[2:], " "), func(t *testing.T) {
			home := anyRowHome(t)
			reapRun(args...)
			base := filepath.Join(home, ".claude/plugins/cache/m/p/1")
			for _, stay := range []string{"skills/pl/SKILL.md", "skills/pl/extra/notes.txt", "agents/pa.md", "skills/plink"} {
				if !lexist(filepath.Join(base, stay)) {
					t.Errorf("a bulk run took the plugin's %s", stay)
				}
			}
			mcp, _ := os.ReadFile(filepath.Join(base, ".mcp.json"))
			if !strings.Contains(string(mcp), "psrv") {
				t.Error("a bulk run edited the plugin's .mcp.json")
			}
			for _, stay := range []string{".claude/skills/ext", "dot/ext/SKILL.md", ".agents/skills/shared/SKILL.md"} {
				if !lexist(filepath.Join(home, stay)) {
					t.Errorf("a bulk run took %s", stay)
				}
			}
		})
	}
	// And it still prunes: the test above must not pass by doing nothing.
	home := anyRowHome(t)
	if code, out := reapRun("prune", "--yes", "--min-sessions", "1"); code != 0 || lexist(filepath.Join(home, ".claude/agents/ag.md")) {
		t.Fatalf("bulk run stopped pruning own items: %d %s", code, out)
	}
}

func TestNamedPruneOfAPluginItemRestoresByteForByte(t *testing.T) {
	cases := []struct {
		item, name, gone string
		byPath           bool
	}{
		{item: "skill by name", name: "p:pl", gone: "skills/pl"},
		{item: "skill by short name", name: "pl", gone: "skills/pl"},
		{item: "skill by selector", name: "claude-code:skill:p:pl", gone: "skills/pl"},
		{item: "skill by path", name: "skills/pl/SKILL.md", gone: "skills/pl", byPath: true},
		{item: "agent by name", name: "p:pa", gone: "agents/pa.md"},
		{item: "agent by path", name: "agents/pa.md", gone: "agents/pa.md", byPath: true},
	}
	for _, c := range cases {
		t.Run(c.item, func(t *testing.T) {
			home := anyRowHome(t)
			base := filepath.Join(home, ".claude/plugins/cache/m/p/1")
			name := c.name
			if c.byPath {
				real, err := filepath.EvalSymlinks(base) // as the report lists it
				if err != nil {
					t.Fatal(err)
				}
				name = filepath.Join(real, c.name)
			}
			before := tree(t, home)
			if code, out := reapRun("prune", name, "--yes"); code != 0 {
				t.Fatalf("prune: %d %s", code, out)
			}
			if lexist(filepath.Join(base, c.gone)) {
				t.Fatalf("%s still there after prune", c.gone)
			}
			if code, out := reapRun("restore", "--all", "--yes"); code != 0 {
				t.Fatalf("restore: %d %s", code, out)
			}
			sameTree(t, before, tree(t, home))
		})
	}
}

func TestNamedDeleteOfAPluginItem(t *testing.T) {
	for name, gone := range map[string]string{"p:pl": "skills/pl", "p:pa": "agents/pa.md"} {
		t.Run(name, func(t *testing.T) {
			home := anyRowHome(t)
			base := filepath.Join(home, ".claude/plugins/cache/m/p/1")
			code, out := reapRun("delete", name, "--yes")
			if code != 0 {
				t.Fatalf("delete: %d %s", code, out)
			}
			if !strings.Contains(out, "update can bring") {
				t.Errorf("no word about plugin updates:\n%s", out)
			}
			if lexist(filepath.Join(base, gone)) {
				t.Fatalf("%s still there", gone)
			}
			if !lexist(filepath.Join(base, ".mcp.json")) {
				t.Fatal("the rest of the plugin went with it")
			}
		})
	}
}

// A plugin update puts a pruned item back, under a new version folder or the same one.
func TestPruneLostAfterAPluginUpdate(t *testing.T) {
	lost := func(t *testing.T, name string) (bool, bool) {
		for _, r := range reportRows(t) {
			if r.Name == name {
				return r.PruneLost, true
			}
		}
		return false, false
	}
	update := func(t *testing.T, home string) {
		t.Helper()
		v2 := filepath.Join(home, ".claude/plugins/cache/m/p/2")
		if err := os.MkdirAll(filepath.Join(v2, "skills/pl"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(v2, "skills/pl/SKILL.md"), []byte("---\nname: pl\ndescription: back again\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(v2, ".mcp.json"), []byte(`{"mcpServers":{"psrv":{"command":"a"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		reg := `{"version":2,"plugins":{"p@m":[{"installPath":"` + v2 + `","installedAt":"2026-01-01T00:00:00Z"}]}}`
		if err := os.WriteFile(filepath.Join(home, ".claude/plugins/installed_plugins.json"), []byte(reg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("new version folder", func(t *testing.T) {
		home := anyRowHome(t)
		if is, found := lost(t, "p:pl"); !found || is {
			t.Fatalf("before any prune: lost=%v found=%v", is, found)
		}
		for _, n := range []string{"p:pl", "psrv"} {
			if code, out := reapRun("prune", n, "--yes"); code != 0 {
				t.Fatalf("prune %s: %d %s", n, code, out)
			}
		}
		if _, found := lost(t, "p:pl"); found {
			t.Fatal("pruned and not back yet, but listed")
		}
		update(t, home)
		for _, n := range []string{"p:pl", "psrv"} {
			if is, found := lost(t, n); !found || !is {
				t.Errorf("%s back with the update: lost=%v found=%v", n, is, found)
			}
		}
		code, out := reapRun("why", "p:pl")
		if code != 0 || !strings.Contains(out, "prune lost") {
			t.Errorf("why says nothing about it: %d %s", code, out)
		}
		code, out = reapRun()
		if code != 0 || !strings.Contains(out, "prune lost") {
			t.Errorf("report says nothing about it: %d %s", code, out)
		}
		// It is a normal row: it can be pruned again.
		if code, out := reapRun("prune", "p:pl", "--yes"); code != 0 {
			t.Errorf("prune again: %d %s", code, out)
		}
	})
	t.Run("own server of the same name is not lost", func(t *testing.T) {
		home := anyRowHome(t)
		if code, out := reapRun("prune", "psrv", "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		}
		cfg := `{"mcpServers":{"psrv":{"command":"mine"}}}`
		if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		if is, found := lost(t, "psrv"); !found || is {
			t.Errorf("lost=%v found=%v", is, found)
		}
	})
	t.Run("restored is not lost", func(t *testing.T) {
		anyRowHome(t)
		if code, out := reapRun("prune", "p:pl", "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		}
		if code, out := reapRun("restore", "--all", "--yes"); code != 0 {
			t.Fatalf("restore: %d %s", code, out)
		}
		if is, found := lost(t, "p:pl"); !found || is {
			t.Errorf("lost=%v found=%v", is, found)
		}
	})
}

func TestLinkRowsLoseTheLinkAndNeverTheTarget(t *testing.T) {
	t.Run("prune and restore a personal link", func(t *testing.T) {
		home := anyRowHome(t)
		before := tree(t, home)
		if code, out := reapRun("prune", "ext", "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		}
		if lexist(filepath.Join(home, ".claude/skills/ext")) {
			t.Fatal("link still there")
		}
		if !lexist(filepath.Join(home, "dot/ext/SKILL.md")) {
			t.Fatal("target removed")
		}
		if code, out := reapRun("restore", "--all", "--yes"); code != 0 {
			t.Fatalf("restore: %d %s", code, out)
		}
		sameTree(t, before, tree(t, home))
	})
	t.Run("prune and restore a link inside a plugin", func(t *testing.T) {
		home := anyRowHome(t)
		before := tree(t, home)
		if code, out := reapRun("prune", "p:plink", "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		}
		if lexist(filepath.Join(home, ".claude/plugins/cache/m/p/1/skills/plink")) || !lexist(filepath.Join(home, "dot/plink/SKILL.md")) {
			t.Fatal("the link should go and the target stay")
		}
		if code, out := reapRun("restore", "--all", "--yes"); code != 0 {
			t.Fatalf("restore: %d %s", code, out)
		}
		sameTree(t, before, tree(t, home))
	})
	t.Run("delete", func(t *testing.T) {
		home := anyRowHome(t)
		if code, out := reapRun("delete", "ext", "--yes"); code != 0 {
			t.Fatalf("delete: %d %s", code, out)
		}
		if lexist(filepath.Join(home, ".claude/skills/ext")) || !lexist(filepath.Join(home, "dot/ext/SKILL.md")) {
			t.Fatal("the link should go and the target stay")
		}
	})
	t.Run("a SKILL.md that is a link", func(t *testing.T) {
		home := anyRowHome(t)
		dir := filepath.Join(home, ".claude/skills/filelink")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(home, "dot/ext/SKILL.md"), filepath.Join(dir, "SKILL.md")); err != nil {
			t.Skip(err)
		}
		before := tree(t, home)
		if code, out := reapRun("prune", "filelink", "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		}
		if lexist(dir) || !lexist(filepath.Join(home, "dot/ext/SKILL.md")) {
			t.Fatal("the folder with the link should go and the target stay")
		}
		if code, out := reapRun("restore", "--all", "--yes"); code != 0 {
			t.Fatalf("restore: %d %s", code, out)
		}
		sameTree(t, before, tree(t, home))
		if code, out := reapRun("delete", "filelink", "--yes"); code != 0 {
			t.Fatalf("delete: %d %s", code, out)
		}
		if b, err := os.ReadFile(filepath.Join(home, "dot/ext/SKILL.md")); err != nil || len(b) == 0 {
			t.Fatalf("target harmed: %v", err)
		}
	})
	t.Run("a whole skills folder that is a link stays refused", func(t *testing.T) {
		home := anyRowHome(t)
		codex := filepath.Join(home, ".codex/skills")
		if err := os.RemoveAll(codex); err != nil {
			t.Fatal(err)
		}
		// Inside the root on purpose: containment alone would let this one through.
		real := filepath.Join(home, ".codex/my-skills")
		if err := os.MkdirAll(filepath.Join(real, "inlink"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(real, "inlink/SKILL.md"), []byte("---\nname: inlink\ndescription: d\n---\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, codex); err != nil {
			t.Skip(err)
		}
		for _, verb := range []string{"prune", "delete"} {
			code, out := reapRun(verb, "codex:skill:inlink", "--yes")
			if code == 0 || !strings.Contains(out, "whole") {
				t.Errorf("%s: %d %s", verb, code, out)
			}
			if !lexist(codex) || !lexist(filepath.Join(real, "inlink/SKILL.md")) {
				t.Fatalf("%s followed the folder link", verb)
			}
		}
	})
	t.Run("mute reaches a link only inside the known folders", func(t *testing.T) {
		home := anyRowHome(t)
		link := filepath.Join(home, ".claude/plugins/cache/m/p/1/skills/pshared")
		if err := os.Symlink(filepath.Join(home, ".agents/skills/shared"), link); err != nil {
			t.Skip(err)
		}
		if code, out := reapRun("mute", "p:pshared", "--yes"); code != 0 {
			t.Fatalf("mute through a shared-folder link: %d %s", code, out)
		}
		before, _ := os.ReadFile(filepath.Join(home, "dot/ext/SKILL.md"))
		if code, out := reapRun("mute", "ext", "--yes"); code == 0 || !strings.Contains(out, "outside") {
			t.Fatalf("mute of a link leading elsewhere: %d %s", code, out)
		}
		after, _ := os.ReadFile(filepath.Join(home, "dot/ext/SKILL.md"))
		if !bytes.Equal(before, after) {
			t.Fatal("target edited")
		}
	})
}

// ~/.agents/skills as another tool declares it: the real folder, with no link.
func TestSharedFolderRows(t *testing.T) {
	t.Run("prune by selector moves the folder and restore brings it back", func(t *testing.T) {
		home := anyRowHome(t)
		before := tree(t, home)
		if code, out := reapRun("prune", "codex:skill:shared", "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		} else if !strings.Contains(out, "other tools") {
			t.Errorf("no word about the other tools:\n%s", out)
		}
		if lexist(filepath.Join(home, ".agents/skills/shared")) {
			t.Fatal("still there")
		}
		if code, out := reapRun("restore", "--all", "--yes"); code != 0 {
			t.Fatalf("restore: %d %s", code, out)
		}
		sameTree(t, before, tree(t, home))
	})
	t.Run("by path, listed under several tools, is one item", func(t *testing.T) {
		home := anyRowHome(t)
		if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(home, ".agents/skills/shared/SKILL.md")
		if code, out := reapRun("prune", path, "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		}
		if lexist(filepath.Dir(path)) {
			t.Fatal("still there")
		}
	})
	t.Run("the bare name still means the link, as before", func(t *testing.T) {
		home := anyRowHome(t)
		if code, out := reapRun("prune", "shared", "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		}
		if lexist(filepath.Join(home, ".claude/skills/shared")) || !lexist(filepath.Join(home, ".agents/skills/shared/SKILL.md")) {
			t.Fatal("the link should go and the folder stay")
		}
	})
	t.Run("delete", func(t *testing.T) {
		home := anyRowHome(t)
		if code, out := reapRun("delete", "codex:skill:shared", "--yes"); code != 0 {
			t.Fatalf("delete: %d %s", code, out)
		}
		if lexist(filepath.Join(home, ".agents/skills/shared")) {
			t.Fatal("still there")
		}
	})
	t.Run("mute", func(t *testing.T) {
		home := anyRowHome(t)
		if code, out := reapRun("mute", "codex:skill:shared", "--yes"); code != 0 {
			t.Fatalf("mute: %d %s", code, out)
		}
		if b, _ := os.ReadFile(filepath.Join(home, ".agents/skills/shared/SKILL.md")); strings.Contains(string(b), "description:") {
			t.Fatal("not muted")
		}
	})
}

func TestPluginServers(t *testing.T) {
	for _, c := range []struct{ name, file string }{{"psrv", ".mcp.json"}, {"pinline", ".claude-plugin/plugin.json"}} {
		t.Run("prune and restore "+c.name, func(t *testing.T) {
			home := anyRowHome(t)
			cfg := filepath.Join(home, ".claude/plugins/cache/m/p/1", c.file)
			before := tree(t, home)
			if code, out := reapRun("prune", c.name, "--yes"); code != 0 {
				t.Fatalf("prune: %d %s", code, out)
			}
			b, _ := os.ReadFile(cfg)
			if strings.Contains(string(b), c.name) {
				t.Fatalf("still in the file: %s", b)
			}
			if !json.Valid(b) {
				t.Fatalf("broken JSON: %s", b)
			}
			if code, out := reapRun("restore", "--all", "--yes"); code != 0 {
				t.Fatalf("restore: %d %s", code, out)
			}
			sameTree(t, before, tree(t, home))
		})
		t.Run("delete "+c.name, func(t *testing.T) {
			home := anyRowHome(t)
			cfg := filepath.Join(home, ".claude/plugins/cache/m/p/1", c.file)
			if code, out := reapRun("delete", c.name, "--yes"); code != 0 {
				t.Fatalf("delete: %d %s", code, out)
			}
			if b, _ := os.ReadFile(cfg); strings.Contains(string(b), c.name) || !json.Valid(b) {
				t.Fatalf("file after delete: %s", b)
			}
		})
	}
	t.Run("declared in both files: the row stays until both are gone", func(t *testing.T) {
		home := anyRowHome(t)
		manifest := filepath.Join(home, ".claude/plugins/cache/m/p/1/.claude-plugin/plugin.json")
		if err := os.WriteFile(manifest, []byte(`{"name":"p","mcpServers":{"psrv":{"command":"a"}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, out := reapRun("prune", "psrv", "--yes"); code != 0 {
			t.Fatalf("prune: %d %s", code, out)
		}
		found := false
		for _, r := range reportRows(t) {
			found = found || (r.Name == "psrv" && strings.HasSuffix(r.Path, "plugin.json"))
		}
		if !found {
			t.Fatal("the copy in plugin.json must still show, not vanish")
		}
	})
}

func TestMuteAPluginAgentByName(t *testing.T) {
	home := anyRowHome(t)
	if code, out := reapRun("mute", "p:pa", "--yes"); code != 0 {
		t.Fatalf("mute: %d %s", code, out)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".claude/plugins/cache/m/p/1/agents/pa.md")); strings.Contains(string(b), "description:") {
		t.Fatal("not muted")
	}
}

// What was never removable by any means stays out, by name too.
func TestNamedPruneStillRefusesWhatHasNoWayOut(t *testing.T) {
	home := anyRowHome(t)
	path := filepath.Join(home, ".claude/CLAUDE.md")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := reapRun("prune", path, "--yes"); code == 0 {
		t.Fatalf("pruned prose: %s", out)
	}
	if !lexist(path) {
		t.Fatal("prose removed")
	}
}
