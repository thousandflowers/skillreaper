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
	"path/filepath"
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

// sqliteCopyLimit bounds the private copy ParseSQLite makes of a database.
const sqliteCopyLimit = 2 << 30

// copySQLite copies a database and its -wal and -shm files, when present, to a
// private temp folder. Querying the copy leaves the other tool's folder exactly
// as it was, and it reads a cleanly closed WAL database the same with every
// sqlite3 build: Apple's refuses one read-only with no -shm beside it, others
// leave -shm and -wal behind. A write racing the copy can only fail the read.
func copySQLite(path string) (copyPath string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "skillreaper-sqlite-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	copyPath = filepath.Join(dir, filepath.Base(path))
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, serr := os.Stat(path + suffix)
		if serr != nil && suffix != "" {
			continue
		}
		if serr != nil || !info.Mode().IsRegular() || info.Size() > sqliteCopyLimit {
			cleanup()
			return "", nil, fmt.Errorf("%s cannot be copied for reading (over %d bytes, or not a regular file)", path+suffix, int64(sqliteCopyLimit))
		}
		if err := copyFile(path+suffix, copyPath+suffix); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return copyPath, cleanup, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// The two layouts OpenCode has used. Today's keeps one row per part in part, and
// a session with no tool call is only visible in session.
const (
	sqliteLegacyQuery = "SELECT session_id, created_at, content FROM messages ORDER BY session_id, created_at;"
	sqlitePartQuery   = `SELECT id, time_updated, '' FROM session WHERE parent_id IS NULL
UNION ALL SELECT session_id, time_created, data FROM part WHERE data LIKE '%"tool"%'
ORDER BY 1, 2;`
)

// sqliteHasTable asks the database whether it has a table.
func sqliteHasTable(ctx context.Context, bin, path, table string) bool {
	out, err := exec.CommandContext(ctx, bin, "-noheader", "-init", os.DevNull, path,
		"SELECT 1 FROM sqlite_master WHERE type='table' AND name='"+table+"';").Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

// ParseSQLite extracts skill/agent/MCP usage from an OpenCode SQLite database
// by querying a private copy of it with the sqlite3 CLI. The real engine is
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

	dbCopy, cleanup, err := copySQLite(path)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), sqliteTimeout)
	defer cancel()

	parts := sqliteHasTable(ctx, bin, dbCopy, "part")
	query := sqliteLegacyQuery
	if parts {
		query = sqlitePartQuery
	}
	// -init os.DevNull: skip ~/.sqliterc so a hostile init file cannot run shell
	// commands or load extensions when we invoke the CLI.
	cmd := exec.CommandContext(ctx, bin, "-noheader", "-init", os.DevNull,
		"-separator", sqliteColSep, "-newline", sqliteRowSep, dbCopy, query)
	// Inherit the parent environment for platform runtime variables such as
	// SystemRoot/TEMP/TMP; bin is already resolved and -init skips user config.

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
	var sessionCount int
	var scanErr error
	if parts {
		// Skill and task tool names and every MCP tool name are from OpenCode's
		// source, not seen in real data here: a fire is proof, no fire is not.
		st.PositiveOnly = true
		st.ToolSep = "_"
		sessionCount, scanErr = scanSQLiteRows(limitedStdout, st, cutoff, partRow)
	} else {
		sessionCount, scanErr = parseSQLiteRows(limitedStdout, st, cutoff)
	}
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
		st.UnparsableLines++
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
	return scanSQLiteRows(r, st, cutoff, messageRow)
}

// sqliteRow turns one row's content into blocks, and says whether the row itself
// marks a session.
type sqliteRow func(content []byte, st *Stats, ts time.Time) (blocks []contentBlock, isSession bool)

// messageRow reads the old layout: one row per message, content a block array.
func messageRow(content []byte, _ *Stats, _ time.Time) ([]contentBlock, bool) {
	var blocks []contentBlock
	if json.Unmarshal(content, &blocks) != nil {
		return nil, true // content that is not a block array (e.g. plain text)
	}
	return blocks, true
}

// partRow reads today's layout. An empty content is a session row. A tool part is
// {"type":"tool","tool":name,"state":{"status":...,"input":...}}, confirmed
// against a real database. MCP tools are named <server>_<tool>.
func partRow(content []byte, st *Stats, ts time.Time) ([]contentBlock, bool) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, true
	}
	var p struct {
		Type  string `json:"type"`
		Tool  string `json:"tool"`
		State struct {
			Status string `json:"status"`
			Input  struct {
				Name         string `json:"name"`
				SubagentType string `json:"subagent_type"`
			} `json:"input"`
		} `json:"state"`
	}
	if json.Unmarshal(content, &p) != nil || p.Type != "tool" {
		return nil, false
	}
	switch p.Tool {
	case "skill":
		if p.State.Status == "error" {
			st.recordError(scan.CatSkill, skillKey(p.State.Input.Name), ts)
		} else {
			st.record(scan.CatSkill, skillKey(p.State.Input.Name), ts)
		}
	case "task":
		st.record(scan.CatAgent, p.State.Input.SubagentType, ts)
	default:
		st.recordTool(scan.SafeText(p.Tool), ts)
	}
	return nil, false
}

func scanSQLiteRows(r io.Reader, st *Stats, cutoff time.Time, decode sqliteRow) (int, error) {
	sessions := map[string]bool{}
	pending := map[string]pendingSkill{}
	mcpPending := map[string]pendingMCP{}
	curSession := ""

	// A Skill or MCP tool_use with no matching result is counted as a use; pending is
	// flushed when the session changes and again at the end.
	flush := func() {
		for id, ps := range pending {
			st.record(scan.CatSkill, ps.key, ps.ts)
			delete(pending, id)
		}
		for id, call := range mcpPending {
			recordMCPCall(st, call.tool, call.ts)
			delete(mcpPending, id)
		}
	}

	br := bufio.NewReaderSize(r, 256*1024)
	overlong := false
	for {
		raw, tooLong, rerr := readDelim(br, sqliteRowSep[0], maxLineBytes)
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			flush()
			return len(sessions), rerr
		}
		if tooLong {
			// The row is unreadable, but the rows behind it are not.
			overlong = true
			st.IncompleteEvidence = true
			continue
		}
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
		blocks, isSession := decode(content, st, ts)
		if isSession {
			sessions[session] = true
		}
		// project "" — OpenCode messages carry no cwd to bucket by; used nil —
		// no init block, so no dead-tool weight.
		recordBlocks(st, blocks, ts, "", pending, mcpPending, nil)
	}
	flush()
	if overlong {
		// Still reported: the caller merges the partial stats and needs to know
		// they are partial.
		return len(sessions), fmt.Errorf("at least one row exceeded the %d byte parser limit and was skipped", maxLineBytes)
	}
	return len(sessions), nil
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
