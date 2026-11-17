package mute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cycle18Skill(t *testing.T, body string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "skills", "x", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

// An unmute that was cut short has put the file back and removed its copy, and
// had not yet taken the entry off the list. The next unmute finds the
// description there and has nothing left to do: it used to fail on the missing
// copy, at every run, with no way to clear the entry.
func TestUnmuteAfterOneThatWasCutShortClearsTheEntry(t *testing.T) {
	const whole = "---\nname: x\ndescription: some words\n---\nbody\n"
	dir, path := cycle18Skill(t, whole)
	if err := Mute(dir, dir, "x", path); err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(dir, "reaped", "muted", "*.bak*"))
	if len(copies) != 1 {
		t.Fatalf("copies kept: %v", copies)
	}
	// What the run that died had done: the file is back, its copy is gone.
	if err := os.WriteFile(path, []byte(whole), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(copies[0], filepath.Join(t.TempDir(), "gone")); err != nil {
		t.Fatal(err)
	}

	if _, err := Unmute(dir, "x"); err != nil {
		t.Fatalf("unmute: %v", err)
	}
	if names, _ := List(dir); len(names) != 0 {
		t.Errorf("still listed as muted: %v", names)
	}
}

// A description in quotes can run on lines that are not indented. All of it is
// the description: taking the first line alone left the rest in the header,
// which no longer parsed.
func TestMuteTakesAQuotedDescriptionWhoseLinesAreNotIndented(t *testing.T) {
	for _, whole := range []string{
		"---\nname: x\ndescription: \"Use this skill when the user asks about\nwidgets, gadgets and \\\"tests\\\"\"\nallowed-tools: Read\n---\nbody\n",
		"---\nname: x\ndescription: 'Use this skill when the user asks about\nwidgets, it''s all here'\nallowed-tools: Read\n---\nbody\n",
		"---\nname: x\ndescription: \"closed on its line\"\nallowed-tools: \"Read\"\n---\nbody\n",
	} {
		dir, path := cycle18Skill(t, whole)
		if err := Mute(dir, dir, "x", path); err != nil {
			t.Fatal(err)
		}
		muted, _ := os.ReadFile(path)
		if got, want := string(muted), "---\nname: x\nallowed-tools: "; !strings.HasPrefix(got, want) {
			t.Errorf("muted, the file reads:\n%s", got)
		}
		if _, err := Unmute(dir, "x"); err != nil {
			t.Fatal(err)
		}
		if back, _ := os.ReadFile(path); string(back) != whole {
			t.Errorf("unmuted, the file reads:\n%s", back)
		}
	}
}

// A file that begins with a byte order mark and was edited while muted gets its
// description back like any other.
func TestUnmuteOfAnEditedFileWithAByteOrderMark(t *testing.T) {
	const whole = "\xef\xbb\xbf---\nname: x\ndescription: some words\n---\nbody\n"
	dir, path := cycle18Skill(t, whole)
	if err := Mute(dir, dir, "x", path); err != nil {
		t.Fatal(err)
	}
	muted, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(muted, "one more line\n"...), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Unmute(dir, "x"); err != nil {
		t.Fatalf("unmute: %v", err)
	}
	if back, _ := os.ReadFile(path); string(back) != whole+"one more line\n" {
		t.Errorf("unmuted, the file reads:\n%q", back)
	}
}

// A quote that closes on the key's own line ends the value there, whatever
// follows it: an apostrophe inside single quotes, a comment that holds a
// quote. Counted as still open, the description read as one that never ends
// and mute said the file had none, or took the next key with it.
func TestMuteReadsAQuoteThatClosesOnItsLine(t *testing.T) {
	for _, description := range []string{
		`'Don't use it for anything else'`,
		`"Use it" # "fixme`,
		`'Don't use it'`,
		`"never closed`,
	} {
		whole := "---\nname: x\ndescription: " + description + "\nlicense: 'MIT' and it's fine\n---\nbody\n"
		dir, path := cycle18Skill(t, whole)
		if err := Mute(dir, dir, "x", path); err != nil {
			t.Errorf("%s: %v", description, err)
			continue
		}
		if muted, _ := os.ReadFile(path); string(muted) != "---\nname: x\nlicense: 'MIT' and it's fine\n---\nbody\n" {
			t.Errorf("%s: muted, the file reads:\n%s", description, muted)
		}
		if _, err := Unmute(dir, "x"); err != nil {
			t.Fatal(err)
		}
		if back, _ := os.ReadFile(path); string(back) != whole {
			t.Errorf("%s: unmuted, the file reads:\n%s", description, back)
		}
	}
}

// With the copy of the description gone and none in the file, unmute says what
// to do: it gave the system's "no such file" and no way out.
func TestUnmuteWithItsCopyGoneSaysWhatToDo(t *testing.T) {
	dir, path := cycle18Skill(t, "---\nname: x\ndescription: some words\n---\nbody\n")
	if err := Mute(dir, dir, "x", path); err != nil {
		t.Fatal(err)
	}
	copies, _ := filepath.Glob(filepath.Join(dir, "reaped", "muted", "*.bak*"))
	if err := os.Rename(copies[0], filepath.Join(t.TempDir(), "gone")); err != nil {
		t.Fatal(err)
	}

	_, err := Unmute(dir, "x")

	if err == nil || !strings.Contains(err.Error(), "write a description back into") {
		t.Errorf("unmute: %v", err)
	}
}

// A mute list that cannot be read is named, with what to do about it: the bare
// "invalid character" of the JSON reader named no file.
func TestACorruptMuteListIsNamed(t *testing.T) {
	dir, path := cycle18Skill(t, "---\nname: x\ndescription: some words\n---\nbody\n")
	state := filepath.Join(dir, "reaped", "muted", "state.json")
	if err := os.MkdirAll(filepath.Dir(state), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Mute(dir, dir, "x", path)

	if err == nil || !strings.Contains(err.Error(), state) || !strings.Contains(err.Error(), "Fix or remove that file") {
		t.Errorf("mute with a corrupt list: %v", err)
	}
}
