// Package prune quarantines unused items reversibly. Nothing is ever
// deleted: files move to <claudeDir>/reaped/, config entries are
// backed up and their payload stored in a manifest so Restore can
// re-insert them.
package prune

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/atomicfile"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

const manifestVersion = 1

// Manifest wraps the list of prune entries together with format
// versioning. When the manifest format changes, bump manifestVersion
// and handle migration in LoadManifest.
type Manifest struct {
	Version       int       `json:"version"`
	QuarantinedAt time.Time `json:"quarantined_at"`
	Entries       []Entry   `json:"entries"`
}

// Entry records one reversible prune action.
type Entry struct {
	ID         string          `json:"id"`
	Category   string          `json:"category"`
	Name       string          `json:"name"`
	From       string          `json:"from,omitempty"` // original path (file/dir moves)
	To         string          `json:"to,omitempty"`   // quarantine path
	ConfigPath string          `json:"configPath,omitempty"`
	JSONScope  string          `json:"jsonScope,omitempty"` // "" = top-level mcpServers, else project path
	Payload    json.RawMessage `json:"payload,omitempty"`   // removed MCP server config
	Timestamp  time.Time       `json:"timestamp"`
	Restored   bool            `json:"restored"`
}

func reapedDir(claudeDir string) string { return filepath.Join(claudeDir, "reaped") }
func manifestPath(claudeDir string) string {
	return filepath.Join(reapedDir(claudeDir), "manifest.json")
}

// LoadManifest returns all recorded prune actions (empty when none).
// Transparently handles the legacy flat-array format (v0) by wrapping
// into a Manifest on read.
func LoadManifest(claudeDir string) ([]Entry, error) {
	b, err := os.ReadFile(manifestPath(claudeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err == nil && m.Version > 0 {
		return m.Entries, nil
	}
	var legacy []Entry
	if err := json.Unmarshal(b, &legacy); err != nil {
		return nil, fmt.Errorf("corrupt manifest %s: %w", manifestPath(claudeDir), err)
	}
	return legacy, nil
}

func saveManifest(claudeDir string, entries []Entry) error {
	if err := os.MkdirAll(reapedDir(claudeDir), 0o755); err != nil {
		return err
	}
	m := Manifest{
		Version:       manifestVersion,
		QuarantinedAt: time.Now(),
		Entries:       entries,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath(claudeDir), b, 0o600)
}

func nextID(entries []Entry) string {
	return fmt.Sprintf("%03d", len(entries)+1)
}

func sanitize(name string) string {
	return strings.NewReplacer(":", "-", "/", "-", "\\", "-").Replace(name)
}

// QuarantineItem moves a skill directory or agent file into the
// quarantine area and records it in the manifest.
func QuarantineItem(claudeDir string, it scan.Item) (Entry, error) {
	src := it.Path
	// A skill is its whole directory, not just SKILL.md.
	if filepath.Base(src) == "SKILL.md" {
		src = filepath.Dir(src)
	}
	if _, err := os.Stat(src); err != nil {
		return Entry{}, err
	}

	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return Entry{}, err
	}

	dest := filepath.Join(reapedDir(claudeDir), string(it.Category), sanitize(it.Name))
	if _, err := os.Stat(dest); err == nil {
		dest = fmt.Sprintf("%s.%d", dest, time.Now().Unix())
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(src, dest); err != nil {
		return Entry{}, err
	}

	e := Entry{
		ID:        nextID(entries),
		Category:  string(it.Category),
		Name:      it.Name,
		From:      src,
		To:        dest,
		Timestamp: time.Now(),
	}
	if err := saveManifest(claudeDir, append(entries, e)); err != nil {
		// The item was moved but cannot be recorded; move it back so it is not
		// lost without a manifest entry to restore it.
		if rbErr := os.Rename(dest, src); rbErr != nil {
			return Entry{}, fmt.Errorf("save manifest: %w (rollback also failed: %v; item left at %s)", err, rbErr, dest)
		}
		return Entry{}, fmt.Errorf("save manifest: %w", err)
	}
	return e, nil
}

// RemoveMCP removes one MCP server from a Claude config file. The
// whole file is backed up first and the removed payload is stored in
// the manifest for restore. scope "" targets the top-level mcpServers;
// otherwise it names a project path under "projects".
func RemoveMCP(claudeDir, configPath, scope, name string) (Entry, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return Entry{}, err
	}

	backupDir := filepath.Join(reapedDir(claudeDir), "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return Entry{}, err
	}
	backup := filepath.Join(backupDir,
		fmt.Sprintf("%s.%d", filepath.Base(configPath), time.Now().UnixNano()))
	if err := os.WriteFile(backup, b, 0o600); err != nil {
		return Entry{}, err
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return Entry{}, fmt.Errorf("unreadable config %s: %w", configPath, err)
	}

	payload, err := removeServerKey(top, scope, name)
	if err != nil {
		return Entry{}, err
	}

	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return Entry{}, err
	}

	// Read the manifest before mutating the config so a manifest-read failure
	// cannot leave the server removed with no record.
	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return Entry{}, err
	}
	if err := atomicfile.Write(configPath, out, 0o600); err != nil {
		return Entry{}, err
	}
	e := Entry{
		ID:         nextID(entries),
		Category:   string(scan.CatMCP),
		Name:       name,
		ConfigPath: configPath,
		JSONScope:  scope,
		Payload:    payload,
		Timestamp:  time.Now(),
	}
	if err := saveManifest(claudeDir, append(entries, e)); err != nil {
		// Restore the original config so the server is not lost without a
		// manifest entry to restore it.
		_ = atomicfile.Write(configPath, b, 0o600)
		return Entry{}, fmt.Errorf("save manifest: %w", err)
	}
	return e, nil
}

// removeServerKey deletes name from the addressed mcpServers object in
// top (mutating top) and returns the removed payload.
func removeServerKey(top map[string]json.RawMessage, scope, name string) (json.RawMessage, error) {
	if scope == "" {
		servers, err := decodeMap(top["mcpServers"])
		if err != nil {
			return nil, err
		}
		payload, ok := servers[name]
		if !ok {
			return nil, fmt.Errorf("mcp server %q not found", name)
		}
		delete(servers, name)
		return payload, encodeInto(top, "mcpServers", servers)
	}

	projects, err := decodeMap(top["projects"])
	if err != nil {
		return nil, err
	}
	proj, err := decodeMap(projects[scope])
	if err != nil {
		return nil, fmt.Errorf("project %q not found: %w", scope, err)
	}
	servers, err := decodeMap(proj["mcpServers"])
	if err != nil {
		return nil, err
	}
	payload, ok := servers[name]
	if !ok {
		return nil, fmt.Errorf("mcp server %q not found in project %q", name, scope)
	}
	delete(servers, name)
	if err := encodeInto(proj, "mcpServers", servers); err != nil {
		return nil, err
	}
	if err := encodeInto(projects, scope, proj); err != nil {
		return nil, err
	}
	return payload, encodeInto(top, "projects", projects)
}

// insertServerKey re-inserts a payload removed by removeServerKey.
func insertServerKey(top map[string]json.RawMessage, scope, name string, payload json.RawMessage) error {
	if scope == "" {
		servers, err := decodeMapOrEmpty(top["mcpServers"])
		if err != nil {
			return err
		}
		servers[name] = payload
		return encodeInto(top, "mcpServers", servers)
	}
	projects, err := decodeMapOrEmpty(top["projects"])
	if err != nil {
		return err
	}
	proj, err := decodeMapOrEmpty(projects[scope])
	if err != nil {
		return err
	}
	servers, err := decodeMapOrEmpty(proj["mcpServers"])
	if err != nil {
		return err
	}
	servers[name] = payload
	if err := encodeInto(proj, "mcpServers", servers); err != nil {
		return err
	}
	if err := encodeInto(projects, scope, proj); err != nil {
		return err
	}
	return encodeInto(top, "projects", projects)
}

func decodeMap(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, errors.New("missing JSON object")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func decodeMapOrEmpty(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	return decodeMap(raw)
}

func encodeInto(target map[string]json.RawMessage, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	target[key] = b
	return nil
}

// Restore undoes one prune action by manifest ID.
func Restore(claudeDir, id string) error {
	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].ID != id {
			continue
		}
		if entries[i].Restored {
			return fmt.Errorf("entry %s already restored", id)
		}
		if err := restoreEntry(claudeDir, &entries[i]); err != nil {
			return err
		}
		return saveManifest(claudeDir, entries)
	}
	return fmt.Errorf("no manifest entry with id %s", id)
}

// withinDir reports whether target resolves to a path at or under root.
func withinDir(root, target string) bool {
	ra, err1 := filepath.Abs(root)
	ta, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(ra, ta)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// RestoreAll undoes every non-restored prune action.
func RestoreAll(claudeDir string) (int, error) {
	entries, err := LoadManifest(claudeDir)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range entries {
		if entries[i].Restored {
			continue
		}
		if err := restoreEntry(claudeDir, &entries[i]); err != nil {
			// Persist the entries already restored so progress is not lost.
			_ = saveManifest(claudeDir, entries)
			return n, err
		}
		n++
	}
	return n, saveManifest(claudeDir, entries)
}

func restoreEntry(claudeDir string, e *Entry) error {
	if len(e.Payload) > 0 {
		// Bound the config write to the install tree (covers ~/.claude.json and
		// plugin .mcp.json under the home dir) so a tampered manifest cannot
		// redirect the write to an arbitrary file.
		if !withinDir(filepath.Dir(claudeDir), e.ConfigPath) {
			return fmt.Errorf("refusing to restore to a path outside %s: %s", filepath.Dir(claudeDir), e.ConfigPath)
		}
		b, err := os.ReadFile(e.ConfigPath)
		if err != nil {
			return err
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(b, &top); err != nil {
			return err
		}
		if err := insertServerKey(top, e.JSONScope, e.Name, e.Payload); err != nil {
			return err
		}
		out, err := json.MarshalIndent(top, "", "  ")
		if err != nil {
			return err
		}
		if err := atomicfile.Write(e.ConfigPath, out, 0o600); err != nil {
			return err
		}
		e.Restored = true
		return nil
	}

	// File move: both the quarantine source and the restore destination must
	// stay within the Claude directory, so a tampered manifest cannot move a
	// file to (or from) an arbitrary location.
	if !withinDir(claudeDir, e.From) || !withinDir(claudeDir, e.To) {
		return fmt.Errorf("refusing to restore outside %s: %s -> %s", claudeDir, e.To, e.From)
	}
	if err := os.MkdirAll(filepath.Dir(e.From), 0o755); err != nil {
		return err
	}
	if err := os.Rename(e.To, e.From); err != nil {
		return err
	}
	e.Restored = true
	return nil
}
