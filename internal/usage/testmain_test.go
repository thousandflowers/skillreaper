package usage

import (
	"os"
	"testing"
)

// TestMain keeps the corpus cache out of the developer's real cache directory:
// its file name hashes a temp path that differs every run, so nothing reuses or
// removes them.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "skillreaper-usage-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", dir)
	os.Setenv("XDG_CACHE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
