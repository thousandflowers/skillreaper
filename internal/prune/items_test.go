package prune

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// Every item gets a path of its own, whatever its name sanitises to and however
// many share it: renaming onto a taken path would destroy the earlier copy.
func TestSameNameItemsDoNotOverwriteEachOther(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	type item struct{ root, path, name string }
	var items []item
	for _, root := range []string{".claude", ".cursor", ".codex", ".config/opencode"} {
		r := filepath.Join(home, root)
		items = append(items, item{r, filepath.Join(r, "agents", "same.md"), "same"})
	}
	for _, name := range []string{"a:b", "a-b", "a\\b"} {
		items = append(items, item{claude, filepath.Join(claude, "agents", name+".md"), name})
	}
	contents := map[string]string{}
	for i, it := range items {
		body := fmt.Sprintf("agent %d\n", i)
		mustWrite(t, it.path, body)
		contents[it.path] = body
	}
	seen := map[string]bool{}
	for _, it := range items {
		e, err := QuarantineItem(claude, scan.Item{Category: scan.CatAgent, Name: it.name, Path: it.path, RootDir: it.root, Removable: true})
		if err != nil {
			t.Fatalf("%s: %v", it.path, err)
		}
		if seen[e.To] {
			t.Fatalf("quarantine path reused: %s", e.To)
		}
		seen[e.To] = true
	}
	n, err := RestoreAll(claude)
	if err != nil || n != len(items) {
		t.Fatalf("RestoreAll = %d, %v", n, err)
	}
	for path, want := range contents {
		if b, err := os.ReadFile(path); err != nil || string(b) != want {
			t.Errorf("%s = %q, %v; want %q", path, b, err, want)
		}
	}
}

// The first quarantined link dangles, so a check that follows links sees a free
// path and the second rename replaces it.
func TestSecondPruneOfALinkKeepsTheFirst(t *testing.T) {
	claude := filepath.Join(t.TempDir(), ".claude")
	skills := filepath.Join(claude, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(skills, "via")
	targets := []string{"../../.agents/skills/via", "../../.agents/skills/via2"}
	var ids []string
	for _, target := range targets {
		if err := os.Symlink(target, link); err != nil {
			t.Skip(err)
		}
		e, err := QuarantineItem(claude, scan.Item{Category: scan.CatSkill, Name: "via", Path: link, Removable: true, LinkTarget: target})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, e.ID)
	}
	for i, id := range ids {
		if err := Restore(claude, id); err != nil {
			t.Fatalf("restore %s: %v", id, err)
		}
		if got, _ := os.Readlink(link); got != targets[i] {
			t.Errorf("restore %s gave %q, want %q", id, got, targets[i])
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
	}
}

// Restore leaves a path that exists again alone, and the others are not held up.
func TestRestoreAllContinuesPastConflicts(t *testing.T) {
	claude := filepath.Join(t.TempDir(), ".claude")
	paths := map[string]string{}
	for _, name := range []string{"a", "b", "c"} {
		paths[name] = filepath.Join(claude, "skills", name, "SKILL.md")
		mustWrite(t, paths[name], "orig "+name)
		if _, err := QuarantineItem(claude, scan.Item{Category: scan.CatSkill, Name: name, Path: paths[name], Removable: true}); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, paths["b"], "the user's new b")
	n, err := RestoreAll(claude)
	var re *RestoreError
	if !errors.As(err, &re) || n != 2 || len(re.Failed) != 1 || re.Failed[0].Entry.Name != "b" {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if !strings.Contains(err.Error(), "exists again") {
		t.Errorf("message does not say what to do: %v", err)
	}
	if b, _ := os.ReadFile(paths["b"]); string(b) != "the user's new b" {
		t.Errorf("b overwritten: %q", b)
	}
	for _, name := range []string{"a", "c"} {
		if b, _ := os.ReadFile(paths[name]); string(b) != "orig "+name {
			t.Errorf("%s = %q", name, b)
		}
	}
	entries, _ := LoadManifest(claude)
	if entries[1].Restored || !entries[0].Restored || !entries[2].Restored {
		t.Errorf("manifest state wrong: %+v", entries)
	}
}

// A run killed after writing its record and before moving leaves a record with
// the item still in place: restore settles it, a later prune replaces it.
func TestRecordOfAMoveThatNeverHappened(t *testing.T) {
	claude := filepath.Join(t.TempDir(), ".claude")
	md := filepath.Join(claude, "skills", "a", "SKILL.md")
	mustWrite(t, md, "orig")
	src := filepath.Dir(md)
	stale := Entry{ID: "001", Category: "skill", Name: "a", From: src, To: filepath.Join(reapedDir(claude), "skill", "a")}
	if err := saveManifest(claude, []Entry{stale}); err != nil {
		t.Fatal(err)
	}
	if n, err := RestoreAll(claude); err != nil || n != 0 {
		t.Fatalf("RestoreAll = %d, %v", n, err)
	}
	if b, _ := os.ReadFile(md); string(b) != "orig" {
		t.Errorf("file disturbed: %q", b)
	}
	if err := saveManifest(claude, []Entry{stale}); err != nil {
		t.Fatal(err)
	}
	e, err := QuarantineItem(claude, scan.Item{Category: scan.CatSkill, Name: "a", Path: md, Removable: true})
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := LoadManifest(claude)
	if !entries[0].Restored || entries[1].Restored || e.ID != "002" {
		t.Errorf("entries = %+v", entries)
	}
}

func TestMoveFailureLeavesNoRecord(t *testing.T) {
	claude := filepath.Join(t.TempDir(), ".claude")
	md := filepath.Join(claude, "skills", "a", "SKILL.md")
	mustWrite(t, md, "orig")
	old := osRename
	osRename = func(from, to string) error {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	defer func() { osRename = old }()
	_, err := QuarantineItem(claude, scan.Item{Category: scan.CatSkill, Name: "a", Path: md, Removable: true})
	if err == nil || !strings.Contains(err.Error(), "different disk") {
		t.Fatalf("err = %v, want it to explain the disks", err)
	}
	if entries, _ := LoadManifest(claude); len(entries) != 0 {
		t.Errorf("record left behind: %+v", entries)
	}
	if b, _ := os.ReadFile(md); string(b) != "orig" {
		t.Errorf("file disturbed: %q", b)
	}
}

const sampleConfig = "{\n    \"z\": \"a&b<c>\",\n    \"mcpServers\": {\n        \"one\": {\"command\": \"x\"},\n        \"two\": {\"command\": \"y\"}\n    },\n    \"a\": 1\n}\n"

// Removing servers and restoring them gives the same bytes and the same mode.
func TestServerRoundTripIsByteIdentical(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".claude.json")
	mustWrite(t, cfg, sampleConfig)
	if err := os.Chmod(cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		if _, err := RemoveMCP(claude, cfg, "", name); err != nil {
			t.Fatal(err)
		}
	}
	mid, _ := os.ReadFile(cfg)
	if !strings.Contains(string(mid), `"a&b<c>"`) || strings.Contains(string(mid), "one") {
		t.Errorf("edit touched the wrong bytes: %s", mid)
	}
	if info, _ := os.Stat(cfg); info.Mode().Perm() != 0o644 {
		t.Errorf("mode changed to %v", info.Mode().Perm())
	}
	if n, err := RestoreAll(claude); err != nil || n != 2 {
		t.Fatalf("RestoreAll = %d, %v", n, err)
	}
	if b, _ := os.ReadFile(cfg); string(b) != sampleConfig {
		t.Errorf("got %s", b)
	}
	if left, _ := os.ReadDir(filepath.Join(claude, "reaped", "backups")); len(left) != 0 {
		t.Errorf("backups left: %v", left)
	}
}

// Restoring in the other order still ends with every server and no other change.
func TestServerRestoreAfterOtherChangesKeepsThem(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".claude.json")
	mustWrite(t, cfg, sampleConfig)
	e, err := RemoveMCP(claude, cfg, "", "one")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(cfg)
	mustWrite(t, cfg, strings.Replace(string(b), `"a": 1`, `"a": 2`, 1))
	if err := Restore(claude, e.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(cfg)
	var v struct {
		A          int
		McpServers map[string]map[string]string
	}
	if err := json.Unmarshal(got, &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 2 || v.McpServers["one"]["command"] != "x" || v.McpServers["two"]["command"] != "y" {
		t.Errorf("got %s", got)
	}
}

func TestServerRestoreRefusesAServerRedefinedSince(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".claude.json")
	mustWrite(t, cfg, `{"mcpServers":{"s":{"command":"old"}}}`)
	e, err := RemoveMCP(claude, cfg, "", "s")
	if err != nil {
		t.Fatal(err)
	}
	mine := `{"mcpServers":{"s":{"command":"mine"}}}`
	mustWrite(t, cfg, mine)
	if err := Restore(claude, e.ID); err == nil || !strings.Contains(err.Error(), "a different s") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(cfg); string(b) != mine {
		t.Errorf("user's server replaced: %s", b)
	}
	entries, _ := LoadManifest(claude)
	if entries[0].Restored {
		t.Error("entry must stay open")
	}
	// The same definition back in the file settles it without a change.
	mustWrite(t, cfg, `{"mcpServers":{"s":{ "command":"old" }}}`)
	if err := Restore(claude, e.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveMCPReportsAVanishedServerAsAlreadyGone(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".claude.json")
	mustWrite(t, cfg, `{"mcpServers":{}}`)
	if _, err := RemoveMCP(claude, cfg, "", "s"); !errors.Is(err, ErrAlreadyGone) {
		t.Errorf("err = %v", err)
	}
}

// A commented config is refused before a backup or a record exists.
func TestRemoveMCPRefusesCommentedConfigCleanly(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	cfg := filepath.Join(home, "c.jsonc")
	body := "// comment\n{\"mcpServers\":{\"s\":{}}}"
	mustWrite(t, cfg, body)
	if _, err := RemoveMCP(claude, cfg, "", "s"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(claude, "reaped", "backups")); !os.IsNotExist(err) {
		t.Errorf("a backup was written for a refused edit: %v", err)
	}
	if b, _ := os.ReadFile(cfg); string(b) != body {
		t.Errorf("config changed: %s", b)
	}
}
