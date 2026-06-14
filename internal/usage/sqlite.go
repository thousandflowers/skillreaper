package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// ErrNoSQLite means the sqlite3 CLI is not on PATH, so OpenCode's SQLite
// session history cannot be read. Callers fall back to REVIEW(no-transcript).
var ErrNoSQLite = errors.New("sqlite3 CLI not found in PATH")

const sqlite3Bin = "sqlite3"

// ASCII record/field separators keep multi-line JSON content on one logical
// row: sqlite3 writes a real newline inside content, so we delimit rows and
// columns with bytes that JSON never contains.
const (
	sqliteRowSep = "\x1e"
	sqliteColSep = "\x1f"
)

// ParseSQLite extracts skill/agent/MCP usage from an OpenCode SQLite database
// by querying it with the sqlite3 CLI in read-only mode. The real engine is
// used on purpose: it reads WAL-mode databases and overflow pages correctly,
// which a hand-rolled page parser would not.
//
// Returns ErrNoSQLite when the CLI is absent; any query or decode failure is
// returned as a wrapped error. It never panics. windowDays is carried into
// Stats; rows whose created_at is parseable and before cutoff are skipped.
func ParseSQLite(path string, cutoff time.Time, windowDays int) (*Stats, error) {
	bin, err := exec.LookPath(sqlite3Bin)
	if err != nil {
		return nil, ErrNoSQLite
	}

	// -readonly: never lock or mutate a database OpenCode may have open.
	query := "SELECT session_id, created_at, content FROM messages ORDER BY session_id, created_at;"
	cmd := exec.Command(bin, "-readonly", "-noheader",
		"-separator", sqliteColSep, "-newline", sqliteRowSep, path, query)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 query failed for %s: %w", path, err)
	}

	st := NewStats(windowDays)
	sessions := map[string]bool{}
	pending := map[string]pendingSkill{}
	curSession := ""

	// A Skill tool_use with no matching result is counted as a use; pending is
	// flushed when the session changes and again at the end.
	flush := func() {
		for id, ps := range pending {
			st.record(scan.CatSkill, ps.key, ps.ts)
			delete(pending, id)
		}
	}

	for _, raw := range bytes.Split(out, []byte(sqliteRowSep)) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		cols := bytes.SplitN(raw, []byte(sqliteColSep), 3)
		if len(cols) < 3 {
			continue
		}
		session := string(bytes.TrimSpace(cols[0]))
		ts := parseSQLiteTime(string(cols[1]))
		content := cols[2]

		if !ts.IsZero() && ts.Before(cutoff) {
			continue
		}
		if session != curSession {
			flush()
			curSession = session
		}
		sessions[session] = true

		var blocks []contentBlock
		if json.Unmarshal(content, &blocks) != nil {
			continue // content that is not a block array (e.g. plain text)
		}
		// project "" — OpenCode messages carry no cwd to bucket by; used nil —
		// no init block, so no dead-tool weight.
		recordBlocks(st, blocks, ts, "", pending, nil)
	}
	flush()

	st.Sessions = len(sessions)
	st.FilesScanned = 1
	return st, nil
}

// parseSQLiteTime reads created_at, which OpenCode may store as RFC3339 or as
// an epoch integer (seconds or milliseconds). Returns the zero time when it
// cannot be parsed, in which case the row is not window-filtered.
func parseSQLiteTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n > 1e12 { // too large for seconds → milliseconds
			return time.UnixMilli(n)
		}
		return time.Unix(n, 0)
	}
	return time.Time{}
}
