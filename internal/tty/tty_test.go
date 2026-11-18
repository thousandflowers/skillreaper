package tty

import (
	"os"
	"runtime"
	"testing"
)

func TestDevNullAndPipesAreNotTerminals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the character-device fallback cannot tell NUL apart")
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	plain, err := os.CreateTemp(t.TempDir(), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	for name, f := range map[string]*os.File{"null": null, "pipe": w, "file": plain} {
		if IsTerminal(f) {
			t.Errorf("%s reported as a terminal", name)
		}
	}
	if IsTerminal(struct{}{}) {
		t.Error("a non-file reported as a terminal")
	}
}
