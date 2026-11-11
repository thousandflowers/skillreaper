package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/menu"
	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
)

// menuRow finds one scanned row of the fake home by category and name.
func menuRow(t *testing.T, rows []report.Row, category, name string) *menu.Row {
	t.Helper()
	for _, r := range rows {
		if string(r.Category) == category && r.Name == name && r.Platform == "claude-code" {
			return menu.NewRow(r)
		}
	}
	t.Fatalf("no %s named %s in the report", category, name)
	return nil
}

func applyOne(t *testing.T, dir string, row *menu.Row, verb string) error {
	t.Helper()
	out := (&menuApplier{dir: dir}).Apply([]menu.Pick{{Row: row, Verb: verb}})
	if len(out) != 1 {
		t.Fatalf("one pick gave %d outcomes", len(out))
	}
	return out[0].Err
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMenuApplierCarriesOutEachVerbAndItsUndo(t *testing.T) {
	// Arrange
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	rows := reportRows(t)

	t.Run("prune moves a skill to quarantine and restore brings it back", func(t *testing.T) {
		skill := filepath.Join(dir, "skills", "mine")

		if err := applyOne(t, dir, menuRow(t, rows, "skill", "mine"), menu.Prune); err != nil {
			t.Fatal(err)
		}

		if lexist(skill) {
			t.Fatal("the pruned skill is still in place")
		}
		entries, err := prune.LoadManifest(dir)
		if err != nil || len(entries) != 1 || entries[0].Name != "mine" {
			t.Fatalf("manifest after a prune: %v %+v", err, entries)
		}
		if err := applyOne(t, dir, menu.PrunedRow(entries[0]), menu.Restore); err != nil {
			t.Fatal(err)
		}
		if !lexist(filepath.Join(skill, "SKILL.md")) || !lexist(filepath.Join(skill, "extra.txt")) {
			t.Fatal("restore did not bring the whole skill back")
		}
	})

	t.Run("mute strips the description and unmute puts it back", func(t *testing.T) {
		file := filepath.Join(dir, "skills", "copied", "SKILL.md")
		before := readFile(t, file)
		row := menuRow(t, rows, "skill", "copied")

		if err := applyOne(t, dir, row, menu.Mute); err != nil {
			t.Fatal(err)
		}

		if strings.Contains(readFile(t, file), "description:") {
			t.Fatal("the muted skill still has its description")
		}
		if err := applyOne(t, dir, row, menu.Mute); err != nil {
			t.Fatalf("muting what is muted already must not fail: %v", err)
		}
		if err := applyOne(t, dir, row, menu.Unmute); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, file); got != before {
			t.Fatalf("unmute left the file different:\n%s", got)
		}
	})

	t.Run("unmute undoes the listed copy and leaves the same name in another tool muted", func(t *testing.T) {
		mine := filepath.Join(dir, "skills", "dup", "SKILL.md")
		other := filepath.Join(home, ".codex", "skills", "dup", "SKILL.md")
		var codex *menu.Row
		for _, r := range rows {
			if r.Name == "dup" && r.Platform == "codex" {
				codex = menu.NewRow(r)
			}
		}
		if codex == nil {
			t.Skip("the fake home has no second copy of dup")
		}
		row := menuRow(t, rows, "skill", "dup")
		if err := applyOne(t, dir, row, menu.Mute); err != nil {
			t.Fatal(err)
		}
		if err := applyOne(t, dir, codex, menu.Mute); err != nil {
			t.Fatal(err)
		}

		if err := applyOne(t, dir, row, menu.Unmute); err != nil {
			t.Fatal(err)
		}

		if !strings.Contains(readFile(t, mine), "description:") {
			t.Fatal("the listed copy is still muted")
		}
		if strings.Contains(readFile(t, other), "description:") {
			t.Fatal("unmuting one row unmuted the copy in another tool")
		}
		if err := applyOne(t, dir, codex, menu.Unmute); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("lock keeps an item and unlock lets it go", func(t *testing.T) {
		row := menuRow(t, rows, "skill", "kept-one")

		if err := applyOne(t, dir, row, menu.Lock); err != nil {
			t.Fatal(err)
		}

		if code, out := reapRun("keep", "--list"); code != 0 || !strings.Contains(out, "kept-one") {
			t.Fatalf("keep --list after a lock: %d %s", code, out)
		}
		if err := applyOne(t, dir, row, menu.Unlock); err != nil {
			t.Fatal(err)
		}
		if _, out := reapRun("keep", "--list"); strings.Contains(out, "kept-one") {
			t.Fatalf("still kept after an unlock: %s", out)
		}
	})

	t.Run("an MCP server leaves its config and comes back whole", func(t *testing.T) {
		config := filepath.Join(home, ".claude.json")

		if err := applyOne(t, dir, menuRow(t, rows, "mcp", "srv"), menu.Prune); err != nil {
			t.Fatal(err)
		}

		if got := readFile(t, config); strings.Contains(got, `"srv"`) || !strings.Contains(got, `"other"`) {
			t.Fatalf("config after pruning one server: %s", got)
		}
		entries, err := prune.LoadManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		var server prune.Entry
		for _, e := range entries {
			if e.Name == "srv" && !e.Restored {
				server = e
			}
		}
		if err := applyOne(t, dir, menu.PrunedRow(server), menu.Restore); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(readFile(t, config), `"srv"`) {
			t.Fatal("restore did not put the server back")
		}
	})

	t.Run("delete removes for good and leaves nothing in quarantine", func(t *testing.T) {
		agent := filepath.Join(dir, "agents", "ag.md")
		before, err := prune.LoadManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		row := menuRow(t, rows, "agent", "ag")

		if err := applyOne(t, dir, row, menu.Delete); err != nil {
			t.Fatal(err)
		}

		after, err := prune.LoadManifest(dir)
		if lexist(agent) || err != nil || len(after) != len(before) {
			t.Fatalf("after a delete: exists=%v err=%v manifest %d -> %d", lexist(agent), err, len(before), len(after))
		}
		if err := applyOne(t, dir, row, menu.Delete); err != nil {
			t.Fatalf("deleting what is gone already must not fail: %v", err)
		}
	})

	t.Run("a locked item refuses prune, mute and delete until it is unlocked", func(t *testing.T) {
		row := menuRow(t, rows, "skill", "dup")
		if err := applyOne(t, dir, row, menu.Lock); err != nil {
			t.Fatal(err)
		}

		for _, verb := range []string{menu.Prune, menu.Mute, menu.Delete} {
			if err := applyOne(t, dir, row, verb); err == nil || !strings.Contains(err.Error(), "locked") {
				t.Fatalf("%s on a locked item: %v", verb, err)
			}
		}

		if !lexist(filepath.Join(dir, "skills", "dup", "SKILL.md")) {
			t.Fatal("a locked item was touched")
		}
	})

	t.Run("an unreadable keep list protects everything", func(t *testing.T) {
		list := filepath.Join(dir, "reaped", "overrides.json")
		saved := readFile(t, list)
		if err := os.WriteFile(list, []byte("{broken"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.WriteFile(list, []byte(saved), 0o600) })

		err := applyOne(t, dir, menuRow(t, rows, "skill", "copied"), menu.Delete)

		if err == nil || !lexist(filepath.Join(dir, "skills", "copied", "SKILL.md")) {
			t.Fatalf("with a broken keep list a delete went through: %v", err)
		}
	})

	t.Run("an action the menu does not know is refused", func(t *testing.T) {
		if err := applyOne(t, dir, menuRow(t, rows, "skill", "copied"), "shred"); err == nil {
			t.Fatal("an unknown action was carried out")
		}
	})
}

func TestMenuApplierTurnsAPluginOffOnceThroughClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude is a shell script")
	}
	// Arrange
	home := deleteHome(t)
	rows := reportRows(t)
	bin := t.TempDir()
	calls := filepath.Join(bin, "calls.log")
	script := "#!/bin/sh\necho \"$@\" >> '" + calls + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	row := menuRow(t, rows, "skill", "p:pl")

	// Act: two rows of the same plugin are one command.
	out := (&menuApplier{dir: filepath.Join(home, ".claude")}).Apply([]menu.Pick{{Row: row, Verb: menu.Disable}, {Row: row, Verb: menu.Disable}})

	// Assert
	if out[0].Err != nil || out[1].Err != nil {
		t.Fatalf("disable: %v %v", out[0].Err, out[1].Err)
	}
	if got := readFile(t, calls); got != "plugin disable p@m\n" {
		t.Fatalf("claude was called with %q", got)
	}
}

func TestMenuApplierSaysWhatToDoWithoutClaude(t *testing.T) {
	// Arrange
	home := deleteHome(t)
	rows := reportRows(t)
	t.Setenv("PATH", t.TempDir())

	// Act
	err := applyOne(t, filepath.Join(home, ".claude"), menuRow(t, rows, "skill", "p:pl"), menu.Disable)

	// Assert
	if err == nil || !strings.Contains(err.Error(), "/plugin") {
		t.Fatalf("with no claude on the PATH: %v", err)
	}
	if !lexist(filepath.Join(home, ".claude/plugins/cache/m/p/1/skills/pl/SKILL.md")) {
		t.Fatal("a failed plugin off touched the plugin's files")
	}
}

func TestMenuOpensOnlyForTheBareNameInATerminal(t *testing.T) {
	cases := []struct {
		name    string
		invoked string
		args    []string
	}{
		{"reap alone is the report", "reap", nil},
		{"skillreaper with an argument is the report", publicName, []string{"--json"}},
		{"tests have no terminal, so the bare name is the report too", publicName, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if opensMenu(c.invoked, c.args) {
				t.Fatal("the menu would open")
			}
		})
	}
}

func TestMenuCommandRefusesWithoutATerminal(t *testing.T) {
	// Arrange
	deleteHome(t)
	var out, errOut bytes.Buffer

	// Act
	code := run([]string{menuCommand}, strings.NewReader(""), &out, &errOut)

	// Assert
	if code != 1 || !strings.Contains(errOut.String(), "cannot open the menu") {
		t.Fatalf("menu with no terminal: %d %q", code, errOut.String())
	}
}

func TestBillingTypeReadsOnlyWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		name, path, want string
	}{
		{"a subscription", write("a.json", `{"oauthAccount":{"billingType":"stripe_subscription"}}`), "stripe_subscription"},
		{"no account", write("b.json", `{"mcpServers":{}}`), ""},
		{"not JSON", write("c.json", `{`), ""},
		{"no file", filepath.Join(dir, "missing.json"), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := billingType(c.path); got != c.want {
				t.Fatalf("billingType = %q, want %q", got, c.want)
			}
		})
	}
}

func TestTypedNameFollowsTheNpmWrapperNotTheBinary(t *testing.T) {
	cases := []struct {
		name, arg0, wrapper, want string
	}{
		{"brew: the name typed is the name run", "/opt/homebrew/bin/skillreaper", "", "skillreaper"},
		{"brew: reap is reap", "/opt/homebrew/bin/reap", "", "reap"},
		{"npm: reap starts the same binary and stays the report", "/pkg/bin/skillreaper", "/usr/local/bin/reap", "reap"},
		{"npm: skillreaper opens the menu", "/pkg/bin/skillreaper", "/usr/local/bin/skillreaper", "skillreaper"},
		{"npx: the wrapper script by its own file name", "/pkg/bin/skillreaper", "/x/_npx/1/node_modules/skillreaper/bin/skillreaper.js", "skillreaper"},
		{"a wrapper under another name says nothing", "/tmp/build/reap", "/tmp/other-tool", "reap"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := typedName(c.arg0, c.wrapper); got != c.want {
				t.Fatalf("typedName(%q, %q) = %q, want %q", c.arg0, c.wrapper, got, c.want)
			}
		})
	}
}
