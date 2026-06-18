package usage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// makeFixtureDB seeds a minimal OpenCode-shaped SQLite database with the
// sqlite3 CLI. It skips the test when the CLI is unavailable (same condition
// ParseSQLite degrades on).
func makeFixtureDB(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	db := filepath.Join(t.TempDir(), "opencode.db")

	// s1: a Skill (tool_use + ok result) and an MCP tool. s2: the same skill
	// once (no id → counted immediately). created_at is epoch seconds.
	seed := `
CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, created_at INTEGER);
INSERT INTO messages (session_id, role, content, created_at) VALUES
 ('s1','assistant','[{"type":"tool_use","id":"t1","name":"Skill","input":{"skill":"oc-skill"}}]', 1717200000),
 ('s1','user','[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]', 1717200001),
 ('s1','assistant','[{"type":"tool_use","name":"mcp__srv__do","input":{}}]', 1717200002),
 ('s2','assistant','[{"type":"tool_use","name":"Skill","input":{"skill":"oc-skill"}}]', 1717200100);
`
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(seed)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed sqlite: %v\n%s", err, out)
	}
	return db
}

func TestParseSQLite(t *testing.T) {
	db := makeFixtureDB(t)

	cutoff := time.Unix(1717100000, 0) // before every fixture row
	st, err := ParseSQLite(db, cutoff, 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.Sessions != 2 {
		t.Errorf("Sessions = %d, want 2", st.Sessions)
	}
	if got := st.Uses[scan.CatSkill]["oc-skill"]; got != 2 {
		t.Errorf("oc-skill uses = %d, want 2", got)
	}
	if got := st.Uses[scan.CatMCP]["srv"]; got != 1 {
		t.Errorf("mcp srv uses = %d, want 1", got)
	}
}

func TestParseSQLiteWindowFilter(t *testing.T) {
	db := makeFixtureDB(t)

	// Cutoff after all rows → everything filtered out, no uses counted.
	cutoff := time.Unix(1717300000, 0)
	st, err := ParseSQLite(db, cutoff, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Uses[scan.CatSkill]["oc-skill"]; got != 0 {
		t.Errorf("oc-skill uses = %d, want 0 (all rows before cutoff)", got)
	}
}

func TestParseSQLiteMultilineContent(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	db := filepath.Join(t.TempDir(), "opencode.db")
	seed := `
CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, created_at INTEGER);
INSERT INTO messages (session_id, role, content, created_at) VALUES
 ('s1','assistant','[
   {"type":"tool_use","name":"Skill","input":{"skill":"multiline-skill"}}
 ]', 1717200000);
`
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(seed)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed sqlite: %v\n%s", err, out)
	}

	st, err := ParseSQLite(db, time.Unix(1717100000, 0), 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Uses[scan.CatSkill]["multiline-skill"]; got != 1 {
		t.Errorf("multiline-skill uses = %d, want 1", got)
	}
}

func TestParseSQLiteOverlongRowReturnsIncompletePartialStats(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	db := filepath.Join(t.TempDir(), "opencode.db")
	hugeInput := strings.Repeat("x", maxLineBytes)
	seed := `
CREATE TABLE messages (id INTEGER PRIMARY KEY, session_id TEXT, role TEXT, content TEXT, created_at INTEGER);
INSERT INTO messages (session_id, role, content, created_at) VALUES
 ('s1','assistant','[{"type":"tool_use","name":"Skill","input":{"skill":"before-overlong"}}]', 1717200000),
 ('s1','assistant','[{"type":"tool_use","name":"Skill","input":{"skill":"overlong","pad":"` + hugeInput + `"}}]', 1717200001);
`
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(seed)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("seed sqlite: %v\n%s", err, out)
	}

	st, err := ParseSQLite(db, time.Unix(1717100000, 0), 30)
	if err == nil {
		t.Fatal("expected ParseSQLite to report an overlong row")
	}
	if st == nil {
		t.Fatal("expected partial stats to be returned")
	}
	if !st.IncompleteEvidence {
		t.Fatal("expected partial stats to be marked incomplete")
	}
	if got := st.Uses[scan.CatSkill]["before-overlong"]; got != 1 {
		t.Fatalf("before-overlong uses = %d, want 1", got)
	}
}

func TestParseSQLiteOutputLimitReturnsIncompletePartialStats(t *testing.T) {
	db := makeFixtureDB(t)
	oldLimit := sqliteOutputLimit
	sqliteOutputLimit = 64
	defer func() { sqliteOutputLimit = oldLimit }()

	st, err := ParseSQLite(db, time.Unix(1717100000, 0), 30)
	if err == nil {
		t.Fatal("expected ParseSQLite to reject output over the byte limit")
	}
	if !strings.Contains(err.Error(), "output exceeded") {
		t.Fatalf("error = %v, want output limit error", err)
	}
	if st == nil {
		t.Fatal("expected partial stats to be returned")
	}
	if !st.IncompleteEvidence {
		t.Fatal("expected partial stats to be marked incomplete")
	}
}

func TestParseSQLiteBadFile(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	bad := filepath.Join(t.TempDir(), "notadb.db")
	if err := os.WriteFile(bad, []byte("this is not a sqlite database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSQLite(bad, time.Unix(0, 0), 30); err == nil {
		t.Error("expected an error for a non-sqlite file, got nil")
	}
}
