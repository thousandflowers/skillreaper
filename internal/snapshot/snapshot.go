// Package snapshot stores a run's --json payload and compares two of them.
//
// Every command answers "how does the stack look now". The questions that
// recur are comparative: did a pruned item come back, is the bloat growing or
// shrinking after a prune, was a prune regretted. A snapshot is the existing
// serialization written to a file, so answering them needs no second
// measurement path and no new parser - only what a run already produces.
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/evidence"
	"github.com/thousandflowers/skillreaper/internal/report"
)

// Dir is where snapshots for one Claude directory live. It sits beside the
// evidence digest and is keyed the same way, so two Claude directories on one
// machine never read each other's history.
func Dir(claudeDir string) string {
	p := evidence.Path(claudeDir)
	if p == "" {
		return ""
	}
	// evidence.Path returns ".../evidence-<key>.json"; reuse <key> verbatim
	// rather than recomputing it, so the two can never disagree about which
	// directory they describe.
	key := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "evidence-"), ".json")
	return filepath.Join(filepath.Dir(p), "snapshots", key)
}

// Save writes r into the snapshot directory, named for the instant it was
// taken, and returns the path. Second-level precision, UTC, and colons
// replaced: the name has to be a filename on Windows too, and has to sort
// chronologically as a string, which is what List relies on.
func Save(claudeDir string, r *report.Report, now time.Time) (string, error) {
	dir := Dir(claudeDir)
	if dir == "" {
		return "", fmt.Errorf("no state directory available for %s", claudeDir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, now.UTC().Format("2006-01-02T150405Z")+".json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// List returns every snapshot path, oldest first.
func List(claudeDir string) ([]string, error) {
	dir := Dir(claudeDir)
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out) // the name format sorts chronologically
	return out, nil
}

// Load reads one snapshot.
func Load(path string) (*report.Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r report.Report
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%s is not a snapshot: %w", filepath.Base(path), err)
	}
	return &r, nil
}
