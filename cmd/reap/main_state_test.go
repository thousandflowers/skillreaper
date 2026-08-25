package main

import (
	"os"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/evidence"
)

// TestMain keeps the suite out of the operator's real state directory.
//
// The evidence digest is written by any run that parses transcripts, including
// the ones these tests drive. Without this, `go test ./...` appends fixture
// items — literally "usedskill" and "skill-0000" — to the digest describing
// the developer's own stack. That happened while this package was being
// written, which is why the override exists and why it is set here rather than
// in each test that happens to call run().
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "skillreaper-test-state")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv(evidence.StateDirEnv, dir); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
