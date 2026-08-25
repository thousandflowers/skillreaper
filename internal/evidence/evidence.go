// Package evidence keeps a durable record of what fired, so verdicts survive
// the deletion of the transcripts they were derived from.
//
// Claude Code removes transcripts older than cleanupPeriodDays (default 30).
// Measured on a real machine: 59 transcripts spanning 24 days, none older than
// the horizon. So the corpus is not a growing history — it is a window sliding
// forward, discarding the far end as fast as it gains at the near one.
//
// The consequence is not "more history would be nice". A skill used less often
// than the retention period has no surviving transcript by the time the next
// run looks, so it is indistinguishable from one installed and forgotten —
// permanently, however long the tool waits. Accumulating a digest is the only
// way that distinction can exist at all.
//
// What is stored is counters and item names. No conversation content ever
// enters this file, which is also what makes it the one artefact here that
// could be shared: transcripts cannot be, and a count can.
package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/thousandflowers/skillreaper/internal/atomicfile"
)

// Format is the on-disk schema version. A mismatch is treated as no digest at
// all rather than partially decoded: half-understood evidence is worse than
// starting over, because it still looks authoritative.
const Format = 1

// Record is what is known about one item across every session ever observed.
type Record struct {
	Uses     int       `json:"uses"`
	Errors   int       `json:"errors"`
	Sessions int       `json:"sessions"` // sessions in which it fired at least once
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
}

// Source records one transcript already folded in, so re-reading it cannot
// count twice.
type Source struct {
	ObservedAt time.Time `json:"observedAt"`
	Sessions   int       `json:"sessions"`
}

// Digest is the accumulated record. Items is category -> name -> Record.
type Digest struct {
	Format  int                          `json:"format"`
	Updated time.Time                    `json:"updated"`
	Sources map[string]Source            `json:"sources"`
	Items   map[string]map[string]Record `json:"items"`
}

// New returns an empty digest ready to merge into.
func New() *Digest {
	return &Digest{
		Format:  Format,
		Sources: map[string]Source{},
		Items:   map[string]map[string]Record{},
	}
}

// StateDirEnv overrides the directory the digest is written to. It exists so a
// test can keep its writes inside its own temporary directory instead of the
// operator's config directory — not a hypothetical: the first draft of this
// package had the unit suite quietly appending fixture skills to the real
// digest, which is how the need was found.
const StateDirEnv = "SKILLREAPER_STATE_DIR"

// Path is where the digest for one Claude directory lives.
//
// Deliberately not inside the Claude directory: reap reads that tree and must
// not write to it. Deliberately not the cache directory either, and that is
// the difference between this and a parse cache — a cache can be rebuilt from
// the corpus and this cannot. Once the retention sweep has run, the digest is
// the only place the older evidence still exists, so it belongs somewhere
// nothing is expected to clear.
//
// Keyed by the Claude directory it describes. One global file would mix
// stacks: pointing --claude-dir at a fixture, a second profile, or a test
// would fold synthetic items into the record of the real one, and nothing
// downstream could separate them again.
//
// Returns "" when the platform offers no config directory, in which case
// accumulation is off rather than silently writing somewhere else.
func Path(claudeDir string) string {
	base := os.Getenv(StateDirEnv)
	if base == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(cfg, "skillreaper")
	}
	sum := sha256.Sum256([]byte(claudeDir))
	return filepath.Join(base, "evidence-"+hex.EncodeToString(sum[:8])+".json")
}

// Load reads the digest. Any problem — absent, unreadable, wrong format,
// malformed — yields an empty digest and never an error: a missing record is
// the normal first run, and a corrupt one must not stop a report.
func Load(path string) *Digest {
	if path == "" {
		return New()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return New()
	}
	var d Digest
	if json.Unmarshal(b, &d) != nil || d.Format != Format {
		return New()
	}
	if d.Sources == nil {
		d.Sources = map[string]Source{}
	}
	if d.Items == nil {
		d.Items = map[string]map[string]Record{}
	}
	return &d
}

// Save writes the digest atomically.
//
// Atomic because of what this file is: once transcripts past the horizon are
// gone, a truncated write does not lose a cache, it loses history nothing can
// regenerate.
func Save(path string, d *Digest) error {
	if path == "" || d == nil {
		return nil
	}
	d.Format = Format
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(path, b, 0o600)
}

// HasSource reports whether this transcript has already been folded in.
func (d *Digest) HasSource(id string) bool {
	_, ok := d.Sources[id]
	return ok
}

// Observation is one transcript's contribution: what fired in it and how often.
type Observation struct {
	// Uses and Errors are category -> name -> count, for this transcript alone.
	Uses   map[string]map[string]int
	Errors map[string]map[string]int
	// At is the transcript's timestamp, used to widen First and Last.
	At time.Time
}

// Add folds one transcript in, keyed by its identity. Adding the same identity
// twice is a no-op, which is what makes running reap repeatedly safe: the
// digest counts transcripts, not readings of them.
func (d *Digest) Add(id string, o Observation) {
	if id == "" || d.HasSource(id) {
		return
	}
	sessions := 0
	if len(o.Uses) > 0 || len(o.Errors) > 0 {
		sessions = 1
	}
	d.Sources[id] = Source{ObservedAt: o.At, Sessions: sessions}

	for cat, byName := range o.Uses {
		for name, n := range byName {
			d.bump(cat, name, n, 0, o.At, true)
		}
	}
	for cat, byName := range o.Errors {
		for name, n := range byName {
			d.bump(cat, name, 0, n, o.At, false)
		}
	}
	if o.At.After(d.Updated) {
		d.Updated = o.At
	}
}

func (d *Digest) bump(cat, name string, uses, errs int, at time.Time, firedHere bool) {
	if d.Items[cat] == nil {
		d.Items[cat] = map[string]Record{}
	}
	r := d.Items[cat][name]
	r.Uses += uses
	r.Errors += errs
	if firedHere {
		r.Sessions++
	}
	if !at.IsZero() {
		if r.First.IsZero() || at.Before(r.First) {
			r.First = at
		}
		if at.After(r.Last) {
			r.Last = at
		}
	}
	d.Items[cat][name] = r
}

// Lookup returns what is known about one item across all observed history.
func (d *Digest) Lookup(cat, name string) (Record, bool) {
	byName, ok := d.Items[cat]
	if !ok {
		return Record{}, false
	}
	r, ok := byName[name]
	return r, ok
}

// SpanDays is how many days separate the earliest and latest observation held.
// This is the number that can exceed the retention horizon, and the whole
// reason the digest exists.
func (d *Digest) SpanDays() int {
	var first, last time.Time
	for _, byName := range d.Items {
		for _, r := range byName {
			if !r.First.IsZero() && (first.IsZero() || r.First.Before(first)) {
				first = r.First
			}
			if r.Last.After(last) {
				last = r.Last
			}
		}
	}
	if first.IsZero() || last.IsZero() {
		return 0
	}
	return int(last.Sub(first).Hours() / 24)
}
