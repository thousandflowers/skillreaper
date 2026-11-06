package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// deleteHome builds a fake home: real items, a plugin, links and sessions.
func deleteHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".cfg"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
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
	md := func(name string) string { return "---\nname: " + name + "\ndescription: some words here\n---\nbody\n" }
	for _, n := range []string{"mine", "kept-one", "copied", "dup"} {
		put(".claude/skills/"+n+"/SKILL.md", md(n))
	}
	put(".claude/skills/mine/extra.txt", "x")
	put(".codex/skills/dup/SKILL.md", md("dup"))
	put(".claude/agents/ag.md", md("ag"))
	put(".claude.json", `{"z":1,"mcpServers":{"srv":{"command":"x"},"other":{"command":"y"}}}`)
	put(".agents/skills/shared/SKILL.md", md("shared"))
	put("dot/ext/SKILL.md", md("ext"))
	plugin := filepath.Join(home, ".claude/plugins/cache/m/p/1")
	put(".claude/plugins/cache/m/p/1/skills/pl/SKILL.md", md("pl"))
	put(".claude/plugins/installed_plugins.json", `{"version":2,"plugins":{"p@m":[{"installPath":"`+plugin+`","installedAt":"2026-01-01T00:00:00Z"}]}}`)
	put(".claude/projects/p/s.jsonl", `{"type":"user","sessionId":"s","timestamp":"2026-11-04T10:00:00Z","message":{"content":"hi"}}`+"\n")
	skills := filepath.Join(home, ".claude/skills")
	for link, target := range map[string]string{
		"shared": filepath.Join(home, ".agents/skills/shared"),
		"ext":    filepath.Join(home, "dot/ext"),
	} {
		if err := os.Symlink(target, filepath.Join(skills, link)); err != nil {
			t.Skip(err)
		}
	}
	return home
}

func TestDelete(t *testing.T) {
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }
	cases := []struct {
		name    string
		arg     string
		setup   func(t *testing.T, home string)
		code    int
		message string
		gone    []string // relative to home
		stays   []string
	}{
		{name: "own skill", arg: "mine", gone: []string{".claude/skills/mine"}, stays: []string{".claude/agents/ag.md"}},
		{name: "own agent", arg: "ag", gone: []string{".claude/agents/ag.md"}},
		{name: "plugin item", arg: "p:pl", message: "an update can bring it back", gone: []string{".claude/plugins/cache/m/p/1/skills/pl"}, stays: []string{".claude/plugins/installed_plugins.json"}},
		{name: "link", arg: "ext", message: "the link goes", gone: []string{".claude/skills/ext"}, stays: []string{"dot/ext/SKILL.md"}},
		{name: "same name", arg: "dup", code: 1, message: "codex:skill:dup", stays: []string{".claude/skills/dup", ".codex/skills/dup"}},
		{name: "selector settles it", arg: "claude-code:dup", gone: []string{".claude/skills/dup"}, stays: []string{".codex/skills/dup"}},
		{name: "link in a known root", arg: "shared", gone: []string{".claude/skills/shared"}, stays: []string{".agents/skills/shared/SKILL.md"}},
		{
			name: "kept", arg: "kept-one", code: 1, message: "kept-one is kept: run reap keep --remove kept-one first",
			setup: func(t *testing.T, home string) {
				var o, e bytes.Buffer
				if c := run([]string{"keep", "kept-one"}, strings.NewReader(""), &o, &e); c != 0 {
					t.Fatalf("keep: %d %s", c, e.String())
				}
			},
			stays: []string{".claude/skills/kept-one"},
		},
		{
			name: "quarantined copy stays", arg: "copied", message: "A copy is in quarantine",
			setup: func(t *testing.T, home string) {
				var o, e bytes.Buffer
				if c := run([]string{"prune", "copied", "--yes"}, strings.NewReader(""), &o, &e); c != 0 {
					t.Fatalf("prune: %d %s", c, e.String())
				}
				md := filepath.Join(home, ".claude/skills/copied/SKILL.md")
				if err := os.MkdirAll(filepath.Dir(md), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(md, []byte("---\nname: copied\ndescription: new\n---\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			gone:  []string{".claude/skills/copied"},
			stays: []string{".claude/reaped/skill/copied/SKILL.md"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := deleteHome(t)
			if c.setup != nil {
				c.setup(t, home)
			}
			var out, errOut bytes.Buffer
			code := run([]string{"delete", c.arg, "--yes"}, strings.NewReader(""), &out, &errOut)
			if code != c.code {
				t.Fatalf("exit = %d, want %d: %s%s", code, c.code, out.String(), errOut.String())
			}
			if !strings.Contains(out.String()+errOut.String(), c.message) {
				t.Errorf("output lacks %q:\n%s%s", c.message, out.String(), errOut.String())
			}
			for _, rel := range c.gone {
				if exists(filepath.Join(home, rel)) {
					t.Errorf("%s still there", rel)
				}
			}
			for _, rel := range c.stays {
				if !exists(filepath.Join(home, rel)) {
					t.Errorf("%s was removed", rel)
				}
			}
		})
	}
}

func TestDeleteServerTouchesNothingElse(t *testing.T) {
	home := deleteHome(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"delete", "srv", "--yes"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("exit = %d: %s", code, errOut.String())
	}
	b, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if string(b) != `{"z":1,"mcpServers":{"other":{"command":"y"}}}` {
		t.Errorf("got %s", b)
	}
}

// It always asks and says it is final; without a yes nothing happens.
func TestDeleteAsks(t *testing.T) {
	home := deleteHome(t)
	skill := filepath.Join(home, ".claude/skills/mine")
	var out, errOut bytes.Buffer
	if code := run([]string{"delete", "mine"}, strings.NewReader("n\n"), &out, &errOut); code != exitNotDone {
		t.Fatalf("declined: exit = %d", code)
	}
	if !strings.Contains(errOut.String(), "cannot be restored") || !strings.Contains(errOut.String(), "[y/N]") {
		t.Errorf("prompt = %q", errOut.String())
	}
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("declined, yet removed: %v", err)
	}

	// A piped answer with no terminal is not an answer.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w.WriteString("y\n")
	w.Close()
	orig := openTTY
	openTTY = func() (*os.File, error) { return nil, os.ErrNotExist }
	t.Cleanup(func() { openTTY = orig })
	if code := run([]string{"delete", "mine"}, r, &out, &errOut); code != exitNotDone {
		t.Fatalf("piped: exit = %d", code)
	}
	if _, err := os.Stat(skill); err != nil {
		t.Fatalf("piped yes removed it: %v", err)
	}
	if code := run([]string{"delete"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Errorf("no name: exit = %d", code)
	}
	if code := run([]string{"delete", "--all", "mine"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Errorf("--all: exit = %d", code)
	}
}

// The checks in prune.DeleteItem hold even for a row that claims otherwise.
func TestDeleteItemBoundaries(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".claude")
	write := func(p string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	outside := write(filepath.Join(home, "elsewhere", "SKILL.md"))
	loose := write(filepath.Join(root, "SKILL.md"))
	inSkills := write(filepath.Join(root, "skills", "SKILL.md"))
	item := func(path string) scan.Item {
		return scan.Item{Category: scan.CatSkill, Name: "x", Path: path, RootDir: root, Removable: true}
	}
	for name, it := range map[string]scan.Item{
		"outside the root":  item(outside),
		"the root itself":   item(loose),
		"the skills folder": item(inSkills),
		"not removable":     {Category: scan.CatSkill, Name: "x", Path: inSkills, RootDir: root},
		"no root to check":  {Category: scan.CatSkill, Name: "x", Path: inSkills, Removable: true},
	} {
		if err := prune.DeleteItem(it); err == nil {
			t.Errorf("%s: deleted", name)
		}
	}
	for _, p := range []string{outside, loose, inSkills} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s removed: %v", p, err)
		}
	}

	// A folder holding a link to something outside: the link goes, its target stays.
	target := write(filepath.Join(home, "precious", "keep.txt"))
	dir := filepath.Join(root, "skills", "withlink")
	write(filepath.Join(dir, "SKILL.md"))
	if err := os.Symlink(filepath.Dir(target), filepath.Join(dir, "out")); err != nil {
		t.Skip(err)
	}
	if err := prune.DeleteItem(item(filepath.Join(dir, "SKILL.md"))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Errorf("folder still there: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("followed the link inside the folder: %v", err)
	}

	// An item whose path is a link: the link alone goes.
	link := filepath.Join(root, "skills", "alink")
	if err := os.Symlink(filepath.Dir(target), link); err != nil {
		t.Skip(err)
	}
	it := item(filepath.Join(link, "SKILL.md"))
	if err := prune.DeleteItem(it); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("link still there: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("target removed: %v", err)
	}
}
