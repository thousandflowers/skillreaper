package mute

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

func writeAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// One name on several platforms, and a skill and an agent sharing a name, are
// separate items: each mutes on its own and each comes back byte for byte.
func TestSameNameItemsMuteSeparately(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	codex := filepath.Join(home, ".codex")
	type item struct{ root, path string }
	items := []item{
		{claude, filepath.Join(claude, "skills", "shared", "SKILL.md")},
		{codex, filepath.Join(codex, "skills", "shared", "SKILL.md")},
		{claude, filepath.Join(claude, "agents", "shared.md")},
	}
	originals := map[string]string{}
	for i, it := range items {
		body := "---\nname: shared\ndescription: copy " + string(rune('a'+i)) + "\n---\nbody\n"
		writeAt(t, it.path, body)
		originals[it.path] = body
	}
	for _, it := range items {
		if err := Mute(claude, it.root, "shared", it.path); err != nil {
			t.Fatalf("Mute %s: %v", it.path, err)
		}
		if b, _ := os.ReadFile(it.path); strings.Contains(string(b), "description:") {
			t.Errorf("%s not stripped", it.path)
		}
	}
	names, _ := List(claude)
	if len(names) != 3 {
		t.Errorf("List = %v, want three entries", names)
	}
	res, err := Unmute(claude, "shared")
	if err != nil || len(res.Restored) != 3 {
		t.Fatalf("Unmute: %+v, %v", res, err)
	}
	for path, want := range originals {
		if b, _ := os.ReadFile(path); string(b) != want {
			t.Errorf("%s = %q, want %q", path, b, want)
		}
	}
}

// A plugin update moves the skill to a new version folder: the old entry must
// neither block a new mute nor stop unmute --all.
func TestEntryWhoseFileIsGone(t *testing.T) {
	claude := filepath.Join(t.TempDir(), ".claude")
	old := filepath.Join(claude, "plugins", "cache", "m", "p", "1.0", "skills", "s", "SKILL.md")
	next := filepath.Join(claude, "plugins", "cache", "m", "p", "2.0", "skills", "s", "SKILL.md")
	other := filepath.Join(claude, "skills", "other", "SKILL.md")
	md := "---\nname: s\ndescription: d\n---\n"
	writeAt(t, old, md)
	writeAt(t, other, md)
	if err := Mute(claude, claude, "p:s", old); err != nil {
		t.Fatal(err)
	}
	if err := Mute(claude, claude, "other", other); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(claude, "plugins", "cache", "m", "p", "1.0")); err != nil {
		t.Fatal(err)
	}
	res, err := UnmuteAll(claude)
	if err != nil || len(res.Restored) != 1 || len(res.Cleared) != 1 {
		t.Fatalf("UnmuteAll: %+v, %v", res, err)
	}
	if _, err := os.Stat(res.Cleared[0].Backup); err != nil {
		t.Errorf("backup of a cleared entry must stay: %v", err)
	}
	if err := Mute(claude, claude, "p:s", old+".missing"); err == nil {
		t.Error("a missing path must be refused")
	}
	writeAt(t, old, md)
	if err := Mute(claude, claude, "p:s", old); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(claude, "plugins", "cache", "m", "p", "1.0")); err != nil {
		t.Fatal(err)
	}
	writeAt(t, next, md)
	if err := Mute(claude, claude, "p:s", next); err != nil {
		t.Fatalf("new version must be mutable: %v", err)
	}
	if names, _ := List(claude); len(names) != 1 {
		t.Errorf("stale entry kept: %v", names)
	}
}

// A muted skill that was pruned stays muted and says what to do.
func TestUnmuteOfAPrunedSkillPointsAtRestore(t *testing.T) {
	claude := filepath.Join(t.TempDir(), ".claude")
	md := filepath.Join(claude, "skills", "s", "SKILL.md")
	orig := "---\nname: s\ndescription: d\n---\nbody\n"
	writeAt(t, md, orig)
	if err := Mute(claude, claude, "s", md); err != nil {
		t.Fatal(err)
	}
	e, err := prune.QuarantineItem(claude, scan.Item{Category: scan.CatSkill, Name: "s", Path: md, Removable: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Unmute(claude, "s")
	if err == nil || !strings.Contains(err.Error(), "reap restore "+e.ID) {
		t.Fatalf("err = %v, want the restore command", err)
	}
	if names, _ := List(claude); len(names) != 1 {
		t.Fatalf("still muted expected, got %v", names)
	}
	if err := prune.Restore(claude, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Unmute(claude, "s"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(md); string(b) != orig {
		t.Errorf("got %q", b)
	}
}

func TestStripOnlyTouchesTheTopLevelDescription(t *testing.T) {
	long := strings.Repeat("x", 1_200_000)
	cases := []struct{ name, in, want string }{
		{"nested", "---\nmetadata:\n  description: keep\ndescription: go\n---\nb\n", "---\nmetadata:\n  description: keep\n---\nb\n"},
		{"block", "---\ndescription: |\n  one\n\n  two\nname: n\n---\n", "---\nname: n\n---\n"},
		{"crlf", "---\r\nname: n\r\ndescription: d\r\n---\r\nb\r\n", "---\r\nname: n\r\n---\r\nb\r\n"},
		{"no final newline", "---\ndescription: d\n---\nbody", "---\n---\nbody"},
		{"long line", "---\ndescription: d\n---\na\n" + long + "\nz\n", "---\n---\na\n" + long + "\nz\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := stripDescription([]byte(c.in))
			if !ok || !bytes.Equal(got, []byte(c.want)) {
				if len(got) > 200 {
					got = got[:200]
				}
				t.Errorf("got %q (found %v)", got, ok)
			}
		})
	}
}

// A file an older version muted, truncated or normalised, still gets its whole
// backup back.
func TestUnmuteRecognisesWhatOlderVersionsWrote(t *testing.T) {
	long := strings.Repeat("x", 1_200_000)
	backups := []string{
		"---\ndescription: d\n---\na\n" + long + "\nz\n",
		"---\r\ndescription: d\r\n---\r\nb\r\n",
		"---\nmetadata:\n  description: kid\ndescription: d\n---\nb",
	}
	for i, backup := range backups {
		got, err := withDescription(legacyStripped([]byte(backup)), []byte(backup), "bak")
		if err != nil || string(got) != backup {
			t.Errorf("case %d: not restored whole (%d bytes, %v)", i, len(got), err)
		}
	}
}

// A run killed between recording a mute and stripping the file leaves an entry
// whose file still has its description; unmute must leave that file as it is.
func TestUnmuteOfAFileNeverStripped(t *testing.T) {
	claude := filepath.Join(t.TempDir(), ".claude")
	md := filepath.Join(claude, "skills", "s", "SKILL.md")
	orig := "---\nname: s\ndescription: d\n---\nbody\n"
	writeAt(t, md, orig)
	if err := Mute(claude, claude, "s", md); err != nil {
		t.Fatal(err)
	}
	writeAt(t, md, orig) // as if the rewrite had not happened
	if _, err := Unmute(claude, "s"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(md); string(b) != orig {
		t.Errorf("got %q", b)
	}
}

// A description that came back ends the mute; muting works on it again.
func TestMuteAgainAfterTheDescriptionCameBack(t *testing.T) {
	claude := filepath.Join(t.TempDir(), ".claude")
	md := filepath.Join(claude, "skills", "s", "SKILL.md")
	v1 := "---\nname: s\ndescription: one\n---\nbody\n"
	v2 := "---\nname: s\ndescription: two\n---\nbody\n"
	writeAt(t, md, v1)
	if err := Mute(claude, claude, "s", md); err != nil {
		t.Fatal(err)
	}
	if err := Mute(claude, claude, "s", md); !errors.Is(err, ErrAlreadyMuted) {
		t.Fatalf("second mute: %v", err)
	}
	writeAt(t, md, v2)
	if !Strippable(md) {
		t.Fatal("the description is back")
	}
	if err := Mute(claude, claude, "s", md); err != nil {
		t.Fatalf("mute after it came back: %v", err)
	}
	if names, _ := List(claude); len(names) != 1 {
		t.Errorf("entries = %v", names)
	}
	if _, err := Unmute(claude, "s"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(md); string(b) != v2 {
		t.Errorf("got %q, want the newer description back", b)
	}
}
