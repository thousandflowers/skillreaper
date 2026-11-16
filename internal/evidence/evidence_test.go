package evidence

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func obs(at time.Time, cat, name string, uses int) Observation {
	return Observation{
		Uses: map[string]map[string]int{cat: {name: uses}},
		At:   at,
	}
}

// The point of the digest is that running reap twice over the same transcripts
// does not inflate the counts. It records transcripts, not readings of them.
func TestAddIsIdempotentPerSource(t *testing.T) {
	d := New()
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	d.Add("t1", obs(at, "skill", "alpha", 3))
	d.Add("t1", obs(at, "skill", "alpha", 3))
	d.Add("t1", obs(at, "skill", "alpha", 99))

	r, ok := d.Lookup("skill", "alpha")
	if !ok {
		t.Fatal("item not recorded")
	}
	if r.Uses != 3 {
		t.Errorf("Uses = %d, want 3 — the same transcript was counted more than once", r.Uses)
	}
	if r.Sessions != 1 {
		t.Errorf("Sessions = %d, want 1", r.Sessions)
	}
}

func TestAddAccumulatesAcrossSources(t *testing.T) {
	d := New()
	jun := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	aug := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	d.Add("t1", obs(jun, "skill", "alpha", 2))
	d.Add("t2", obs(aug, "skill", "alpha", 5))

	r, _ := d.Lookup("skill", "alpha")
	if r.Uses != 7 {
		t.Errorf("Uses = %d, want 7", r.Uses)
	}
	if r.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2", r.Sessions)
	}
	if !r.First.Equal(jun) {
		t.Errorf("First = %v, want %v", r.First, jun)
	}
	if !r.Last.Equal(aug) {
		t.Errorf("Last = %v, want %v", r.Last, aug)
	}
}

func TestErrorsAreCountedWithoutCountingAsAFiring(t *testing.T) {
	d := New()
	d.Add("t1", Observation{
		Errors: map[string]map[string]int{"skill": {"broken": 2}},
		At:     time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
	})
	r, ok := d.Lookup("skill", "broken")
	if !ok {
		t.Fatal("an item that only errored was not recorded")
	}
	if r.Errors != 2 {
		t.Errorf("Errors = %d, want 2", r.Errors)
	}
	if r.Uses != 0 {
		t.Errorf("Uses = %d, want 0 — an error is not a use", r.Uses)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv(StateDirEnv, t.TempDir())
	path := Path("/some/claude/dir")

	d := New()
	d.Add("t1", obs(time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), "agent", "planner", 4))
	if err := Save(path, d); err != nil {
		t.Fatal(err)
	}

	back := Load(path)
	r, ok := back.Lookup("agent", "planner")
	if !ok || r.Uses != 4 {
		t.Fatalf("round trip lost the record: %+v ok=%v", r, ok)
	}
	if !back.HasSource("t1") {
		t.Error("the source list did not survive, so the next run would double count")
	}
}

// Every failure to read yields an empty digest, never an error: a missing
// record is the normal first run, and a corrupt one must not stop a report.
func TestLoadFailuresYieldAnEmptyDigest(t *testing.T) {
	dir := t.TempDir()

	if d := Load(filepath.Join(dir, "absent.json")); len(d.Items) != 0 {
		t.Error("a missing file did not yield an empty digest")
	}
	if d := Load(""); len(d.Items) != 0 {
		t.Error("an empty path did not yield an empty digest")
	}

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d := Load(corrupt); len(d.Items) != 0 {
		t.Error("a corrupt file did not yield an empty digest")
	}

	wrongFormat := filepath.Join(dir, "old.json")
	if err := os.WriteFile(wrongFormat, []byte(`{"format":999,"items":{"skill":{"x":{"uses":1}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if d := Load(wrongFormat); len(d.Items) != 0 {
		t.Error("a future format version was partially decoded instead of ignored")
	}
}

// Two Claude directories must not share a digest, or a fixture run folds
// synthetic items into the record describing the real stack.
func TestPathIsPerClaudeDirectory(t *testing.T) {
	t.Setenv(StateDirEnv, t.TempDir())
	a := Path("/home/someone/.claude")
	b := Path("/tmp/fixture/.claude")
	if a == b {
		t.Fatalf("both directories map to %s", a)
	}
	if Path("/home/someone/.claude") != a {
		t.Error("the same directory did not map to a stable path")
	}
}

func TestStateDirEnvRedirectsTheWrite(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(StateDirEnv, dir)
	p := Path("/anything")
	if filepath.Dir(p) != dir {
		t.Errorf("path %s is not under the overridden state dir %s", p, dir)
	}
}

// A skill is invoked by its name in whatever case the person typed, and its
// folder may spell it another way. The window folds case when it matches a use
// to an item; the durable record did not, so a use it remembered was not found
// and the item read as never used.
func TestLookupFoldFindsAUseRecordedInAnotherCase(t *testing.T) {
	last := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	d := &Digest{Items: map[string]map[string]Record{"skill": {
		"case-skill": {Uses: 2, Last: last.AddDate(0, 0, -3)},
		"CASE-SKILL": {Uses: 1, Last: last},
		"other":      {Uses: 9},
	}}}
	r, ok := d.LookupFold("skill", "Case-Skill")
	if !ok || r.Uses != 3 || !r.Last.Equal(last) {
		t.Errorf("LookupFold = %+v, %v", r, ok)
	}
	if _, ok := d.LookupFold("skill", "missing"); ok {
		t.Error("a name nobody used was found")
	}
	if _, ok := d.LookupFold("agent", "case-skill"); ok {
		t.Error("another category's record was found")
	}
}

// The parser credits a tool call to what stands before the first "__". A server
// named a__b finds its calls under their whole names, and in a record written
// before those were kept, under that first part.
func TestLookupServerFindsCallsOfANameWithTheSeparator(t *testing.T) {
	last := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	d := &Digest{Items: map[string]map[string]Record{"mcp": {
		"a":           {Uses: 5},
		"a__b__tool":  {Uses: 2, Last: last},
		"a__b__other": {Uses: 1},
		"plain":       {Uses: 4},
	}}}
	if r, ok := d.LookupServer("mcp", "a__b"); !ok || r.Uses != 3 || !r.Last.Equal(last) {
		t.Errorf("a__b = %+v, %v", r, ok)
	}
	if r, ok := d.LookupServer("mcp", "plain"); !ok || r.Uses != 4 {
		t.Errorf("plain = %+v, %v", r, ok)
	}
	if r, ok := d.LookupServer("mcp", "a"); !ok || r.Uses != 5 {
		t.Errorf("a = %+v, %v", r, ok)
	}
	older := &Digest{Items: map[string]map[string]Record{"mcp": {"a": {Uses: 5}}}}
	if r, ok := older.LookupServer("mcp", "a__b"); !ok || r.Uses != 5 {
		t.Errorf("a__b in an older record = %+v, %v", r, ok)
	}
	if _, ok := d.LookupServer("mcp", "none__x"); ok {
		t.Error("a server nobody called was found")
	}
}
