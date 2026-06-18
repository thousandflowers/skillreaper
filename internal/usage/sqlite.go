package usage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// sqliteTimeout bounds the sqlite3 subprocess so a huge or pathological
// database cannot hang reap indefinitely.
const sqliteTimeout = 60 * time.Second

// sqliteOutputLimit bounds total sqlite3 stdout even though rows are streamed.
var sqliteOutputLimit int64 = 50 << 20

// ErrNoSQLite means the sqlite3 CLI is not on PATH, so OpenCode's SQLite
// session history cannot be read. Callers fall back to REVIEW(no-transcript).
var ErrNoSQLite = errors.New("sqlite3 CLI not found in PATH")

var errSQLiteOutputLimit = errors.New("sqlite3 query output exceeded byte limit")

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

	ctx, cancel := context.WithTimeout(context.Background(), sqliteTimeout)
	defer cancel()

	// -readonly: never lock or mutate a database OpenCode may have open.
	// -init os.DevNull: skip ~/.sqliterc so a hostile init file cannot run shell
	// commands or load extensions when we invoke the CLI.
	query := "SELECT session_id, created_at, content FROM messages ORDER BY session_id, created_at;"
	cmd := exec.CommandContext(ctx, bin, "-readonly", "-noheader", "-init", os.DevNull,
		"-separator", sqliteColSep, "-newline", sqliteRowSep, path, query)
	// Run with a minimal environment so an inherited PATH or HOME cannot change
	// which binary runs or which config files it reads. TMPDIR is kept so
	// sqlite3 can spill a large sort to disk.
	cmd.Env = []string{}
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		cmd.Env = append(cmd.Env, "TMPDIR="+tmp)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 stdout pipe failed for %s: %w", path, err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sqlite3 start failed for %s: %w", path, err)
	}

	st := NewStats(windowDays)
	limitedStdout := &byteLimitReader{r: stdout, limit: sqliteOutputLimit}
	sessionCount, scanErr := parseSQLiteRows(limitedStdout, st, cutoff)
	if scanErr != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("sqlite3 query timed out after %s for %s", sqliteTimeout, path)
	}
	if scanErr != nil {
		st.Sessions = sessionCount
		st.FilesScanned = 1
		st.MalformedLines++
		st.IncompleteEvidence = true
		if errors.Is(scanErr, errSQLiteOutputLimit) {
			return st, fmt.Errorf("sqlite3 query output exceeded %d bytes for %s", sqliteOutputLimit, path)
		}
		return st, fmt.Errorf("sqlite3 query output could not be parsed for %s: %w", path, scanErr)
	}
	if waitErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("sqlite3 query timed out after %s for %s", sqliteTimeout, path)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("sqlite3 query failed for %s: %w: %s", path, waitErr, msg)
		}
		return nil, fmt.Errorf("sqlite3 query failed for %s: %w", path, waitErr)
	}

	st.Sessions = sessionCount
	st.FilesScanned = 1
	return st, nil
}

type byteLimitReader struct {
	r     io.Reader
	limit int64
	read  int64
}

func (r *byteLimitReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.read += int64(n)
	if r.read > r.limit {
		return n, errSQLiteOutputLimit
	}
	return n, err
}

func parseSQLiteRows(r io.Reader, st *Stats, cutoff time.Time) (int, error) {
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

	sc := bufio.NewScanner(r)
	sc.Split(splitSQLiteRows)
	sc.Buffer(make([]byte, 0, 256*1024), maxLineBytes)
	for sc.Scan() {
		raw := sc.Bytes()
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
	if err := sc.Err(); err != nil {
		flush()
		return len(sessions), err
	}
	flush()

	return len(sessions), nil
}

func splitSQLiteRows(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, sqliteRowSep[0]); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		if len(data) == 0 {
			return 0, nil, nil
		}
		return len(data), data, nil
	}
	return 0, nil, nil
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
