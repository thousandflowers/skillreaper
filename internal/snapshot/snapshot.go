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
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/atomicfile"
	"github.com/thousandflowers/skillreaper/internal/evidence"
	"github.com/thousandflowers/skillreaper/internal/report"
)

const (
	// format is written into every snapshot. Files from before it have none.
	format     = 1
	timeLayout = "2006-01-02T150405Z"
)

// namePattern is what Save writes. A file named otherwise in the folder (a
// backup someone put there) is not a snapshot, and sorting it among them would
// decide which two get compared.
var namePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T\d{6}Z)(_\d+)?\.json$`)

// Snapshot is one saved run.
type Snapshot struct {
	*report.Report
	// Format is the snapshot layout, 0 on files from before it was recorded.
	Format int `json:"SnapshotFormat,omitempty"`
	// Settings are the options that judged this run, by flag.
	Settings map[string]string `json:"SnapshotSettings,omitempty"`
}

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
// chronologically as a string, which is what List relies on. A name already
// taken within the same second gets a numeric suffix that still sorts after it
// ('_' comes after '.'), since replacing it would lose the state before a prune.
func Save(claudeDir string, r *report.Report, now time.Time, settings map[string]string) (string, error) {
	dir := Dir(claudeDir)
	if dir == "" {
		return "", fmt.Errorf("no state directory available for %s", claudeDir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	copied := *r
	if copied.Rows == nil {
		copied.Rows = []report.Row{} // null means "not a snapshot" to Load
	}
	b, err := json.MarshalIndent(Snapshot{Report: &copied, Format: format, Settings: settings}, "", "  ")
	if err != nil {
		return "", err
	}
	base := now.UTC().Format(timeLayout)
	for n := 1; ; n++ {
		name := base + ".json"
		if n > 1 {
			name = fmt.Sprintf("%s_%02d.json", base, n)
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		f.Close()
		if err := atomicfile.Write(path, b, 0o600); err != nil {
			os.Remove(path)
			return "", err
		}
		return path, nil
	}
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
		if !e.IsDir() && namePattern.MatchString(e.Name()) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out) // the name format sorts chronologically
	return out, nil
}

// Newest returns up to n usable snapshots, oldest of them first, and one line for
// each it had to pass over. A corrupt or future-dated file cannot take over
// bare reap diff for good.
func Newest(claudeDir string, n int, now time.Time) (paths, skipped []string, err error) {
	all, err := List(claudeDir)
	if err != nil {
		return nil, nil, err
	}
	for i := len(all) - 1; i >= 0 && len(paths) < n; i-- {
		name := filepath.Base(all[i])
		stamp := namePattern.FindStringSubmatch(name)[1]
		if t, err := time.Parse(timeLayout, stamp); err != nil || t.After(now) {
			skipped = append(skipped, name+": it is dated in the future")
			continue
		}
		if _, err := Load(all[i]); err != nil {
			skipped = append(skipped, err.Error())
			continue
		}
		paths = append(paths, all[i])
	}
	slices.Reverse(paths)
	return paths, skipped, nil
}

// Name is how a snapshot is referred to: its filename without the extension.
func Name(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".json")
}

// Resolve turns what was typed into a path: a name as the diff prints it, with or
// without .json, or a path as given.
func Resolve(claudeDir, arg string) string {
	if _, err := os.Stat(arg); err == nil || strings.ContainsRune(arg, filepath.Separator) {
		return arg
	}
	return filepath.Join(Dir(claudeDir), strings.TrimSuffix(arg, ".json")+".json")
}

// Load reads one snapshot, and refuses a file that only happens to be JSON: any
// object decodes without error, and an empty stack compared against a real one
// reads as everything gone.
func Load(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := Snapshot{Report: &report.Report{}}
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s is not a snapshot: %w", filepath.Base(path), err)
	}
	bad := func(why string) (*Snapshot, error) {
		return nil, fmt.Errorf("%s is not a snapshot: %s", filepath.Base(path), why)
	}
	switch {
	case s.Report == nil, s.GeneratedAt.IsZero():
		return bad("it has no date")
	case s.Rows == nil:
		return bad("it has no rows")
	case s.Format > format:
		return bad("it was written by a newer version")
	case s.DeadTokensPerSession < 0, s.MuteTokensPerSession < 0:
		return bad("its totals are negative")
	}
	return &s, nil
}

// Warnings says why two snapshots may not be comparable.
func Warnings(from, to *Snapshot) []string {
	var out []string
	if from.GeneratedAt.After(to.GeneratedAt) {
		out = append(out, fmt.Sprintf("the first snapshot is the newer one (%s, %s), so this reads backwards",
			from.GeneratedAt.UTC().Format(time.DateTime), to.GeneratedAt.UTC().Format(time.DateTime)))
	}
	if from.WindowDays != to.WindowDays && (from.Settings == nil || to.Settings == nil) {
		out = append(out, fmt.Sprintf("different settings: --days %d and %d; verdicts moved because of that may not be real", from.WindowDays, to.WindowDays))
	}
	if from.Settings == nil || to.Settings == nil {
		return out
	}
	for _, name := range slices.Sorted(maps.Keys(from.Settings)) {
		if a, b := from.Settings[name], to.Settings[name]; b != "" && a != b {
			out = append(out, fmt.Sprintf("different settings: %s %s and %s; verdicts moved because of that may not be real", name, a, b))
		}
	}
	return out
}
