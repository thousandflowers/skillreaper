package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A folder name can hold escape sequences (a cloned repo, a plugin): the path
// lines reap prints, the one a delete asks y/N under among them, carry none.
func TestPathsPrintedCarryNoControlCharacters(t *testing.T) {
	home := deleteHome(t)
	evil := "evil\x1b]0;PWNED\x07\x1b[31m"
	skill := filepath.Join(home, ".claude", "skills", evil, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("---\nname: x\ndescription: words\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdated(t, skill)
	shown := "evil?]0;PWNED??[31m"
	for _, args := range [][]string{{"delete", shown}, {"keep", shown}} {
		_, out := reapRun(args...)
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Errorf("%v printed a control character:\n%q", args, out)
		}
	}
}
