package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

func writeCacheFixture(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A cache that does not invalidate is worse than no cache: it serves stale
// verdicts silently. Every axis that can change the evidence must change the
// fingerprint.
func TestFingerprintChangesWithTheCorpus(t *testing.T) {
	dir := t.TempDir()
	writeCacheFixture(t, dir, "a.jsonl", "{}\n")

	base := Fingerprint([]string{dir}, 30)
	if base == "" {
		t.Fatal("fingerprint of a readable corpus must not be empty")
	}

	if again := Fingerprint([]string{dir}, 30); again != base {
		t.Error("fingerprint changed with nothing else changing")
	}

	if narrow := Fingerprint([]string{dir}, 7); narrow == base {
		t.Error("a different window must not reuse the same parse")
	}

	// A new session is a new file.
	writeCacheFixture(t, dir, "b.jsonl", "{}\n")
	added := Fingerprint([]string{dir}, 30)
	if added == base {
		t.Error("fingerprint unchanged after a transcript was added")
	}

	// Appending to an existing transcript changes size and mtime.
	p := writeCacheFixture(t, dir, "a.jsonl", "{}\n{}\n")
	if grown := Fingerprint([]string{dir}, 30); grown == added {
		t.Error("fingerprint unchanged after a transcript grew")
	}

	// Same size, later mtime: a rewrite in place must still invalidate.
	sameSize := Fingerprint([]string{dir}, 30)
	later := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if touched := Fingerprint([]string{dir}, 30); touched == sameSize {
		t.Error("fingerprint unchanged after mtime moved with size held")
	}
}

// A corpus that cannot be walked completely must not get a fingerprint at all:
// caching a partial parse under a name that looks whole is the failure this
// guards against.
func TestFingerprintEmptyOnUnwalkableDir(t *testing.T) {
	if fp := Fingerprint([]string{filepath.Join(t.TempDir(), "missing")}, 30); fp != "" {
		t.Errorf("got %q, want empty for a missing directory", fp)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := NewStats(30)
	st.Sessions = 42
	st.record(scan.CatSkill, "some-skill", time.Now())

	SaveCache(dir, "fp-1", st)

	got, ok := LoadCache(dir, "fp-1")
	if !ok {
		t.Fatal("round trip missed")
	}
	if got.Sessions != 42 {
		t.Errorf("Sessions = %d, want 42", got.Sessions)
	}
	if got.Uses[scan.CatSkill]["some-skill"] != 1 {
		t.Error("recorded use did not survive the round trip")
	}
}

func TestCacheMissesAreSafe(t *testing.T) {
	dir := t.TempDir()

	if _, ok := LoadCache(dir, "fp-1"); ok {
		t.Error("hit with no cache file present")
	}

	SaveCache(dir, "fp-1", NewStats(30))

	if _, ok := LoadCache(dir, "fp-2"); ok {
		t.Error("hit on a different fingerprint")
	}
	if _, ok := LoadCache(dir, ""); ok {
		t.Error("hit on an empty fingerprint, which means do-not-cache")
	}

	if err := os.WriteFile(CachePath(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCache(dir, "fp-1"); ok {
		t.Error("hit on a corrupt cache file")
	}
}

// An empty fingerprint means the corpus could not be described, so nothing
// should be written for it.
func TestSaveIsSkippedWithoutAFingerprint(t *testing.T) {
	dir := t.TempDir()
	SaveCache(dir, "", NewStats(30))
	if _, err := os.Stat(CachePath(dir)); !os.IsNotExist(err) {
		t.Error("a cache file was written under an empty fingerprint")
	}
}
