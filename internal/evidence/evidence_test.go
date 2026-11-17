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
	if r, ok := d.LookupServer("mcp", "a__b", nil); !ok || r.Uses != 3 || !r.Last.Equal(last) {
		t.Errorf("a__b = %+v, %v", r, ok)
	}
	if r, ok := d.LookupServer("mcp", "plain", nil); !ok || r.Uses != 4 {
		t.Errorf("plain = %+v, %v", r, ok)
	}
	if r, ok := d.LookupServer("mcp", "a", nil); !ok || r.Uses != 5 {
		t.Errorf("a = %+v, %v", r, ok)
	}
	older := &Digest{Items: map[string]map[string]Record{"mcp": {"a": {Uses: 5}}}}
	if r, ok := older.LookupServer("mcp", "a__b", nil); !ok || r.Uses != 5 {
		t.Errorf("a__b in an older record = %+v, %v", r, ok)
	}
	if _, ok := d.LookupServer("mcp", "none__x", nil); ok {
		t.Error("a server nobody called was found")
	}
}

// A server's record is what was filed under its own name (a resource read, or
// a call the parser credited rightly) and the calls kept whole that are its own.
func TestLookupServerAddsTheExactNameAndTheCallsKeptWhole(t *testing.T) {
	d := &Digest{Items: map[string]map[string]Record{"mcp": {
		"a__b":          {Uses: 1}, // ReadMcpResourceTool names the server itself
		"a__b__tool":    {Uses: 2},
		"docs":          {Uses: 3}, // the parser's credit for docs___search
		"docs___search": {Uses: 3},
		"___t":          {Uses: 4},
		"plain":         {Uses: 5},
		"plain__x__y":   {Uses: 5}, // kept whole, and credited to plain already
	}}}
	for name, want := range map[string]int{"a__b": 3, "docs_": 3, "docs": 3, "_": 4, "plain": 5} {
		if r, ok := d.LookupServer("mcp", name, nil); !ok || r.Uses != want {
			t.Errorf("%s = %+v, %v, want %d uses", name, r, ok, want)
		}
	}
	older := &Digest{Items: map[string]map[string]Record{"mcp": {"docs": {Uses: 3}}}}
	if r, ok := older.LookupServer("mcp", "docs_", nil); !ok || r.Uses != 3 {
		t.Errorf("docs_ in an older record = %+v, %v", r, ok)
	}
}

// With the configured servers known, a call kept whole goes to the longest name
// that begins it, and is taken off the shorter name it was filed under.
func TestLookupServerLeavesACallToTheServerItBelongsTo(t *testing.T) {
	d := &Digest{Items: map[string]map[string]Record{"mcp": {
		"a":             {Uses: 2}, // filed here by the parser, for a__b's two calls
		"a__b__tool":    {Uses: 2},
		"docs":          {Uses: 4}, // three of its own, one of docs_
		"docs___search": {Uses: 1},
	}}}
	servers := []string{"a", "a__b", "docs", "docs_"}
	for name, want := range map[string]int{"a": 0, "a__b": 2, "docs": 3, "docs_": 1} {
		r, ok := d.LookupServer("mcp", name, servers)
		if r.Uses != want || ok != (want > 0) {
			t.Errorf("%s = %d uses, %v; want %d", name, r.Uses, ok, want)
		}
	}
}
