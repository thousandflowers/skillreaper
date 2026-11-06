// Package mute reduces a skill's always-injected weight by stripping the
// description from its SKILL.md frontmatter, leaving the skill installed and
// invokable. The original file is backed up so the action is reversible.
package mute

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/atomicfile"
	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/safepath"
)

func mutedDir(claudeDir string) string  { return filepath.Join(claudeDir, "reaped", "muted") }
func statePath(claudeDir string) string { return filepath.Join(mutedDir(claudeDir), "state.json") }

// backupName is the backup filename for one muted file. Distinct names can
// sanitize to the same string (e.g. "a:b" and "a-b") and one name can sit on
// several platforms; a hash of the name and path keeps their backups distinct,
// so muting one never clobbers another's.
func backupName(name, path string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name + "\x00" + path))
	return fmt.Sprintf("%s-%08x.md.bak", safepath.Sanitize(name), h.Sum32())
}

const maxStateFileSize = 10 << 20

var writeStateFile = func(claudeDir string, b []byte) error {
	return safepath.AtomicWriteFileWithin(claudeDir, statePath(claudeDir), b, 0o600)
}

// resolveMutableExisting confines target to the mutable roots of one platform
// config directory. The parameter is a platform root, not necessarily the
// Claude directory: passing claudeDir unconditionally is what made muting an
// item from another platform impossible (#41).
func resolveMutableExisting(rootDir, target string) (string, error) {
	tr, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(tr)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to use non-regular file %s", target)
	}
	if rel, ok := resolvedRel(filepath.Join(rootDir, "skills"), tr); ok && isPersonalSkillPath(rel) {
		return tr, nil
	}
	if rel, ok := resolvedRel(filepath.Join(rootDir, "agents"), tr); ok && isPersonalAgentPath(rel) {
		return tr, nil
	}
	if rel, ok := resolvedRel(filepath.Join(rootDir, "plugins"), tr); ok && isPluginSkillOrAgentPath(rel) {
		return tr, nil
	}
	// A personal skill linked in from a shared skills folder: its SKILL.md
	// lives there, not under skills/, and muting it edits it there.
	for _, home := range safepath.LinkedSkillHomes(rootDir) {
		if rel, ok := resolvedRel(home, tr); ok && isPersonalSkillPath(rel) {
			return tr, nil
		}
	}
	return "", fmt.Errorf("refusing to use path outside mutable skill, agent, or plugin roots: %s", target)
}

func resolvedRel(root, target string) (string, bool) {
	rootInfo, err := os.Lstat(root)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", false
	}
	rr, err := filepath.EvalSymlinks(root)
	if err != nil || !safepath.WithinDir(rr, target) {
		return "", false
	}
	rel, err := filepath.Rel(rr, target)
	if err != nil {
		return "", false
	}
	return rel, true
}

func relParts(rel string) []string {
	if rel == "." {
		return nil
	}
	return strings.Split(rel, string(filepath.Separator))
}

func isPersonalSkillPath(rel string) bool {
	parts := relParts(rel)
	return len(parts) == 2 && parts[1] == "SKILL.md"
}

func isPersonalAgentPath(rel string) bool {
	parts := relParts(rel)
	return len(parts) == 1 && strings.HasSuffix(parts[0], ".md")
}

func isPluginSkillOrAgentPath(rel string) bool {
	parts := relParts(rel)
	for i := 0; i < len(parts); i++ {
		if parts[i] == "skills" && i+2 == len(parts)-1 && parts[len(parts)-1] == "SKILL.md" {
			return true
		}
		if parts[i] == "agents" && i+1 == len(parts)-1 && strings.HasSuffix(parts[len(parts)-1], ".md") {
			return true
		}
	}
	return false
}

// ErrAlreadyMuted is returned by Mute when this file is already muted, so bulk
// callers can skip it with errors.Is rather than matching a string.
var ErrAlreadyMuted = errors.New("already muted")

// ErrNoDescription is returned by Mute when the file has nothing to strip.
var ErrNoDescription = errors.New("no description to strip")

// Entry records one muted skill so it can be restored.
type Entry struct {
	Name   string `json:"name,omitempty"`
	Path   string `json:"path"`   // original SKILL.md
	Backup string `json:"backup"` // backup copy of the original file
	// Root is the platform config directory Path was confined against when it
	// was muted. Empty on entries written before multi-platform support, in
	// which case the Claude directory is assumed — which is what those entries
	// were in fact confined against.
	Root string `json:"root,omitempty"`
}

// State holds the mute entries, keyed by the muted file's resolved path: one
// name can be a skill and an agent, or sit on several platforms, and each copy
// has its own state. Entries written by older versions are keyed by name and
// carry no Name field; loadState fills it in.
type State struct {
	Muted map[string]Entry `json:"muted"`
}

func loadState(claudeDir string) (*State, error) {
	b, err := safepath.ReadRegularFileWithin(claudeDir, statePath(claudeDir), maxStateFileSize)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Muted: map[string]Entry{}}, nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Muted == nil {
		s.Muted = map[string]Entry{}
	}
	for key, e := range s.Muted {
		if e.Name == "" {
			e.Name = key
			s.Muted[key] = e
		}
	}
	return &s, nil
}

func saveState(claudeDir string, s *State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeStateFile(claudeDir, b)
}

// Mute strips a skill or agent description, keeping a backup.
//
// rootDir is the platform config directory the item was found under, and is
// what skillPath is confined against. It is not always claudeDir: since #29
// the report covers every detected platform, and an item under another
// platform's root would otherwise be refused — a refusal the caller treats as
// fatal, stopping a multi-platform run partway through. Backups and state
// stay in claudeDir regardless, the way the prune manifest does.
func Mute(claudeDir, rootDir, name, skillPath string) error {
	if rootDir == "" {
		rootDir = claudeDir
	}
	resolvedSkillPath, err := resolveMutableExisting(rootDir, skillPath)
	if err != nil {
		return err
	}
	s, err := loadState(claudeDir)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(resolvedSkillPath)
	if err != nil {
		return err
	}
	_, hasDescription := stripDescription(b)
	for key, e := range s.Muted {
		// A file still without its description is muted. One that has a description
		// again lost its mute, and muting it starts over, with a new backup.
		if e.Path == resolvedSkillPath && !hasDescription {
			return fmt.Errorf("%w: %s", ErrAlreadyMuted, name)
		}
		// The same name at a path that is gone: a plugin update moved it to a new
		// version folder. The old entry can only get in the way.
		if e.Path == resolvedSkillPath || (e.Name == name && vanished(claudeDir, e)) {
			delete(s.Muted, key)
		}
	}
	// Preserve the original file mode so muting does not silently widen (or
	// tighten) the skill's permissions.
	info, err := os.Stat(resolvedSkillPath)
	if err != nil {
		return err
	}
	perm := info.Mode().Perm()
	stripped, ok := stripDescription(b)
	if !ok {
		return fmt.Errorf("%w in %s", ErrNoDescription, skillPath)
	}
	backup := freeBackupPath(claudeDir, name, resolvedSkillPath)
	// Backups hold the user's original content; keep them owner-only.
	if err := safepath.AtomicWriteFileWithin(claudeDir, backup, b, 0o600); err != nil {
		return err
	}
	// Recorded before the file changes. A run killed in between leaves an entry
	// whose file still has its description, which unmute handles; the other way
	// round leaves a stripped file nothing points back to.
	s.Muted[resolvedSkillPath] = Entry{Name: name, Path: resolvedSkillPath, Backup: backup, Root: rootDir}
	if err := saveState(claudeDir, s); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("save mute state: %w", err)
	}
	if err := atomicfile.Write(resolvedSkillPath, stripped, perm); err != nil {
		delete(s.Muted, resolvedSkillPath)
		if saveErr := saveState(claudeDir, s); saveErr != nil {
			return fmt.Errorf("%w (and its record could not be removed: %v)", err, saveErr)
		}
		_ = os.Remove(backup)
		return err
	}
	return nil
}

// freeBackupPath does not reuse a backup left by an entry dropped earlier: it
// may be the only copy of what that file used to say.
func freeBackupPath(claudeDir, name, path string) string {
	base := filepath.Join(mutedDir(claudeDir), backupName(name, path))
	for n := 1; ; n++ {
		p := base
		if n > 1 {
			p = fmt.Sprintf("%s.%d", base, n)
		}
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return p
		}
	}
}

// Strippable reports whether the file has a description for Mute to strip.
func Strippable(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	_, ok := stripDescription(b)
	return ok
}

// Result is what an unmute did.
type Result struct {
	Restored []Entry
	// Cleared are entries whose file no longer exists. The backup stays on disk.
	Cleared []Entry
}

// Unmute restores every muted copy with this name. What cannot be restored stays
// muted and is returned as an error; the others are done regardless.
func Unmute(claudeDir, name string) (Result, error) {
	s, err := loadState(claudeDir)
	if err != nil {
		return Result{}, err
	}
	found := false
	for _, e := range s.Muted {
		found = found || e.Name == name
	}
	if !found {
		return Result{}, fmt.Errorf("not muted: %s", name)
	}
	return unmuteWhere(claudeDir, s, func(e Entry) bool { return e.Name == name })
}

// UnmuteAll restores every muted file it can, and returns what it could not as
// an error alongside what it did.
func UnmuteAll(claudeDir string) (Result, error) {
	s, err := loadState(claudeDir)
	if err != nil {
		return Result{}, err
	}
	return unmuteWhere(claudeDir, s, func(Entry) bool { return true })
}

func unmuteWhere(claudeDir string, s *State, match func(Entry) bool) (Result, error) {
	keys := make([]string, 0, len(s.Muted))
	for key := range s.Muted {
		keys = append(keys, key)
	}
	sort.Strings(keys) // deterministic order
	var res Result
	var errs []error
	for _, key := range keys {
		e := s.Muted[key]
		if !match(e) {
			continue
		}
		switch {
		case vanished(claudeDir, e):
			res.Cleared = append(res.Cleared, e)
		default:
			if err := restoreWithin(claudeDir, e); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", e.Name, err))
				continue
			}
			res.Restored = append(res.Restored, e)
		}
		delete(s.Muted, key)
	}
	if err := saveState(claudeDir, s); err != nil {
		errs = append(errs, err)
	}
	return res, errors.Join(errs...)
}

// vanished reports an entry whose file is gone for good, as opposed to one that
// is only in quarantine and comes back with reap restore.
func vanished(claudeDir string, e Entry) bool {
	if _, err := os.Lstat(e.Path); !os.IsNotExist(err) || !insideItsRoot(claudeDir, e) {
		return false
	}
	_, quarantined := quarantineID(claudeDir, e.Path)
	return !quarantined
}

// insideItsRoot keeps a state file pointing elsewhere a refusal, not a cleanup.
func insideItsRoot(claudeDir string, e Entry) bool {
	root := e.Root
	if root == "" {
		root = claudeDir
	}
	for _, r := range append(safepath.LinkedSkillHomes(root), root) {
		if safepath.WithinDir(resolvedParent(r), e.Path) {
			return true
		}
	}
	return false
}

// quarantineID finds the prune entry holding path.
func quarantineID(claudeDir, path string) (string, bool) {
	entries, err := prune.LoadManifest(claudeDir)
	if err != nil {
		return "", true // cannot tell, so nothing is treated as gone for good
	}
	for _, e := range entries {
		if e.Restored {
			continue
		}
		for _, from := range []string{e.From, e.FilesFrom} {
			if from != "" && safepath.WithinDir(resolvedParent(from), path) {
				return e.ID, true
			}
		}
	}
	return "", false
}

// resolvedParent resolves what still exists of a path whose last part was moved.
func resolvedParent(path string) string {
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return path
	}
	return filepath.Join(dir, filepath.Base(path))
}

// List returns the names of all currently muted skills, one per muted file.
func List(claudeDir string) ([]string, error) {
	entries, err := Entries(claudeDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names, nil
}

// Entries returns what is muted, by name and then path.
func Entries(claudeDir string) ([]Entry, error) {
	s, err := loadState(claudeDir)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(s.Muted))
	for _, e := range s.Muted {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func restoreWithin(claudeDir string, e Entry) error {
	root := e.Root
	if root == "" {
		root = claudeDir
	}
	if _, err := os.Lstat(e.Path); os.IsNotExist(err) {
		if id, _ := quarantineID(claudeDir, e.Path); id != "" {
			return fmt.Errorf("it is in quarantine: run reap restore %s, then reap unmute %s", id, e.Name)
		}
	}
	path, err := resolveMutableExisting(root, e.Path)
	if err != nil {
		return err
	}
	backup, err := resolveBackupExisting(claudeDir, e.Backup)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(backup)
	if err != nil {
		return err
	}
	// Restore at the skill's current mode so unmute preserves permissions.
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	restored, err := withDescription(current, b, backup)
	if err != nil {
		return err
	}
	if err := atomicfile.Write(path, restored, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Remove(backup)
}

// withDescription returns what the skill's file should hold after an unmute. A
// file untouched since the mute gets its backup back. An edited one keeps the
// edits and gets the description back, unless it has written a new one. A file
// with no header to hold it is refused, naming the backup, which stays.
func withDescription(current, backup []byte, backupPath string) ([]byte, error) {
	stripped, removed, _ := splitDescription(backup)
	if bytes.Equal(current, stripped) || bytes.Equal(current, legacyStripped(backup)) {
		return backup, nil
	}
	if _, _, has := splitDescription(current); has {
		return current, nil
	}
	lines := bytes.SplitAfter(current, []byte("\n"))
	if len(lines) == 0 || string(bytes.TrimSpace(lines[0])) != "---" {
		return nil, fmt.Errorf("the file has changed and has no header to put the description in; it is saved in %s", backupPath)
	}
	for i := 1; i < len(lines); i++ {
		if string(bytes.TrimSpace(lines[i])) == "---" {
			var out bytes.Buffer
			out.Write(bytes.Join(lines[:i], nil))
			out.Write(removed)
			out.Write(bytes.Join(lines[i:], nil))
			return out.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("the file has changed and its header does not close; the description is saved in %s", backupPath)
}

func resolveBackupExisting(claudeDir, target string) (string, error) {
	absMuted, err1 := filepath.Abs(mutedDir(claudeDir))
	absTarget, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil || !safepath.WithinDir(absMuted, absTarget) {
		return "", fmt.Errorf("refusing to use backup outside %s: %s", mutedDir(claudeDir), target)
	}
	return safepath.ExistingRegularFileWithin(claudeDir, target)
}

// stripDescription removes the "description:" line(s) from a Markdown file's
// YAML frontmatter, leaving the rest of the file (the name line and body)
// untouched. The second result is false when there was no frontmatter
// description to strip.
func stripDescription(b []byte) ([]byte, bool) {
	out, _, found := splitDescription(b)
	return out, found
}

// splitDescription returns the file without its description, and the lines that
// held it. Only the header is looked at and only those lines change: the rest
// of the file, line endings included, comes through byte for byte.
func splitDescription(b []byte) (rest, removed []byte, found bool) {
	return splitKey(b, false)
}

// splitKey does the work. anyIndent also matches an indented description key,
// which is another key's child: only what older versions wrote needs that.
func splitKey(b []byte, anyIndent bool) (rest, removed []byte, found bool) {
	lines := bytes.SplitAfter(b, []byte("\n"))
	if string(bytes.TrimSpace(lines[0])) != "---" {
		return b, nil, false
	}
	var out, taken bytes.Buffer
	out.Write(lines[0])
	dropping := false // dropping continuation lines of a removed description
	keyIndent := 0
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		trimmed := bytes.TrimSpace(line)
		if dropping {
			// A block/folded scalar's value sits on continuation lines
			// indented deeper than the key (blank lines belong to it too).
			// A line at the key's indent or shallower ends the value.
			if len(trimmed) == 0 || indentOf(string(line)) > keyIndent {
				taken.Write(line)
				continue
			}
			dropping = false
		}
		if string(trimmed) == "---" {
			out.Write(bytes.Join(lines[i:], nil))
			return out.Bytes(), taken.Bytes(), found
		}
		if bytes.HasPrefix(trimmed, []byte("description:")) && (anyIndent || indentOf(string(line)) == 0) {
			found = true
			keyIndent = indentOf(string(line))
			dropping = true // also drop any continuation lines that follow
			taken.Write(line)
			continue
		}
		out.Write(line)
	}
	return out.Bytes(), taken.Bytes(), found
}

// legacyLineLimit is where older versions stopped reading a file.
const legacyLineLimit = 1 << 20

// legacyStripped is what an older version wrote when it muted this file: it also
// took indented description keys, normalised line endings and dropped everything
// from the first line over a mebibyte on. A file muted back then still has to be
// recognised as untouched, or its unmute would keep that damage.
func legacyStripped(backup []byte) []byte {
	rest, _, _ := splitKey(backup, true)
	var out bytes.Buffer
	for _, line := range bytes.SplitAfter(rest, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		if len(line) > legacyLineLimit {
			break
		}
		out.Write(bytes.TrimRight(line, "\r\n"))
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// indentOf returns the number of leading whitespace characters in line.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}
