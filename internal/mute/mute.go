// Package mute reduces a skill's always-injected weight by stripping the
// description from its SKILL.md frontmatter, leaving the skill installed and
// invokable. The original file is backed up so the action is reversible.
package mute

import (
	"bufio"
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
	"github.com/thousandflowers/skillreaper/internal/safepath"
)

func mutedDir(claudeDir string) string  { return filepath.Join(claudeDir, "reaped", "muted") }
func statePath(claudeDir string) string { return filepath.Join(mutedDir(claudeDir), "state.json") }

// backupName is the backup filename for a muted skill. Distinct names can
// sanitize to the same string (e.g. "a:b" and "a-b"); a hash of the original
// name keeps their backups distinct so muting one never clobbers the other's.
func backupName(name string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return fmt.Sprintf("%s-%08x.md.bak", safepath.Sanitize(name), h.Sum32())
}

func resolveExistingWithin(root, target string) (string, error) {
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	tr, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	if !safepath.WithinDir(rr, tr) {
		return "", fmt.Errorf("refusing to use path outside %s: %s", root, target)
	}
	info, err := os.Stat(tr)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to use non-regular file %s", target)
	}
	return tr, nil
}

func resolveMutableExisting(claudeDir, target string) (string, error) {
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
	if rel, ok := resolvedRel(filepath.Join(claudeDir, "skills"), tr); ok && isPersonalSkillPath(rel) {
		return tr, nil
	}
	if rel, ok := resolvedRel(filepath.Join(claudeDir, "agents"), tr); ok && isPersonalAgentPath(rel) {
		return tr, nil
	}
	if rel, ok := resolvedRel(filepath.Join(claudeDir, "plugins"), tr); ok && isPluginSkillOrAgentPath(rel) {
		return tr, nil
	}
	return "", fmt.Errorf("refusing to use path outside mutable skill, agent, or plugin roots: %s", target)
}

func resolvedRel(root, target string) (string, bool) {
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

// ErrAlreadyMuted is returned by Mute when the named skill is already muted,
// so bulk callers can skip it with errors.Is rather than matching a string.
var ErrAlreadyMuted = errors.New("already muted")

// Entry records one muted skill so it can be restored.
type Entry struct {
	Path   string `json:"path"`   // original SKILL.md
	Backup string `json:"backup"` // backup copy of the original file
}

// State maps a skill name to its mute Entry.
type State struct {
	Muted map[string]Entry `json:"muted"`
}

func loadState(claudeDir string) (*State, error) {
	b, err := os.ReadFile(statePath(claudeDir))
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
	return &s, nil
}

func saveState(claudeDir string, s *State) error {
	if err := os.MkdirAll(mutedDir(claudeDir), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// Kept as a direct write on purpose: Mute's rollback path depends on a
	// saveState failure being injectable (read-only file), and an atomic
	// rename would bypass that. The live SKILL.md and backup use atomicfile.
	return os.WriteFile(statePath(claudeDir), b, 0o600)
}

// Mute strips the description field from a skill's SKILL.md frontmatter,
// backing up the original first. It errors if the skill is already muted or
// has no description to strip.
func Mute(claudeDir, name, skillPath string) error {
	resolvedSkillPath, err := resolveMutableExisting(claudeDir, skillPath)
	if err != nil {
		return err
	}
	s, err := loadState(claudeDir)
	if err != nil {
		return err
	}
	if _, ok := s.Muted[name]; ok {
		return fmt.Errorf("%w: %s", ErrAlreadyMuted, name)
	}
	b, err := os.ReadFile(resolvedSkillPath)
	if err != nil {
		return err
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
		return fmt.Errorf("no description to strip in %s", skillPath)
	}
	if err := os.MkdirAll(mutedDir(claudeDir), 0o755); err != nil {
		return err
	}
	backup := filepath.Join(mutedDir(claudeDir), backupName(name))
	// Backups hold the user's original content; keep them owner-only.
	if err := atomicfile.Write(backup, b, 0o600); err != nil {
		return err
	}
	if err := atomicfile.Write(resolvedSkillPath, stripped, perm); err != nil {
		return err
	}
	s.Muted[name] = Entry{Path: resolvedSkillPath, Backup: backup}
	if err := saveState(claudeDir, s); err != nil {
		// The skill was stripped but the mute cannot be recorded; restore the
		// original so it is not left silently degraded with no way to unmute,
		// and drop the now-orphan backup so no state-less copy lingers.
		if rbErr := atomicfile.Write(resolvedSkillPath, b, perm); rbErr != nil {
			return fmt.Errorf("save mute state: %w (rollback also failed: %v; skill left stripped at %s)", err, rbErr, resolvedSkillPath)
		}
		_ = os.Remove(backup)
		return fmt.Errorf("save mute state: %w", err)
	}
	return nil
}

// Unmute restores one muted skill's SKILL.md from its backup.
func Unmute(claudeDir, name string) error {
	s, err := loadState(claudeDir)
	if err != nil {
		return err
	}
	e, ok := s.Muted[name]
	if !ok {
		return fmt.Errorf("not muted: %s", name)
	}
	if err := restoreWithin(claudeDir, e); err != nil {
		return err
	}
	delete(s.Muted, name)
	return saveState(claudeDir, s)
}

// UnmuteAll restores every muted skill and returns the count restored.
func UnmuteAll(claudeDir string) (int, error) {
	s, err := loadState(claudeDir)
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(s.Muted))
	for name := range s.Muted {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic order
	n := 0
	for _, name := range names {
		if err := restoreWithin(claudeDir, s.Muted[name]); err != nil {
			// Persist the skills already restored so progress is not lost.
			_ = saveState(claudeDir, s)
			return n, err
		}
		delete(s.Muted, name)
		n++
	}
	return n, saveState(claudeDir, s)
}

// List returns the names of all currently muted skills.
func List(claudeDir string) ([]string, error) {
	s, err := loadState(claudeDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(s.Muted))
	for name := range s.Muted {
		names = append(names, name)
	}
	return names, nil
}

func restoreWithin(claudeDir string, e Entry) error {
	path, err := resolveMutableExisting(claudeDir, e.Path)
	if err != nil {
		return err
	}
	backup, err := resolveExistingWithin(mutedDir(claudeDir), e.Backup)
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
	if err := atomicfile.Write(path, b, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Remove(backup)
}

// stripDescription removes the "description:" line(s) from a Markdown file's
// YAML frontmatter, leaving the rest of the file (the name line and body)
// untouched. The second result is false when there was no frontmatter
// description to strip.
func stripDescription(b []byte) ([]byte, bool) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var out bytes.Buffer
	first := true
	inHeader := false
	found := false
	dropping := false // dropping continuation lines of a removed description
	keyIndent := 0
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if first {
			first = false
			if trimmed == "---" {
				inHeader = true
			}
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		if inHeader {
			if dropping {
				// A block/folded scalar's value sits on continuation lines
				// indented deeper than the key (blank lines belong to it too).
				// A line at the key's indent or shallower ends the value.
				if trimmed == "" || indentOf(line) > keyIndent {
					continue
				}
				dropping = false
			}
			if trimmed == "---" {
				inHeader = false
				out.WriteString(line)
				out.WriteByte('\n')
				continue
			}
			if strings.HasPrefix(trimmed, "description:") {
				found = true
				keyIndent = indentOf(line)
				dropping = true // also drop any continuation lines that follow
				continue
			}
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes(), found
}

// indentOf returns the number of leading whitespace characters in line.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}
