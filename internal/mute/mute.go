// Package mute reduces a skill's always-injected weight by stripping the
// description from its SKILL.md frontmatter, leaving the skill installed and
// invokable. The original file is backed up so the action is reversible.
package mute

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func mutedDir(claudeDir string) string  { return filepath.Join(claudeDir, "reaped", "muted") }
func statePath(claudeDir string) string { return filepath.Join(mutedDir(claudeDir), "state.json") }

func sanitize(name string) string {
	return strings.NewReplacer(":", "-", "/", "-", "\\", "-").Replace(name)
}

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
	return os.WriteFile(statePath(claudeDir), b, 0o644)
}

// Mute strips the description field from a skill's SKILL.md frontmatter,
// backing up the original first. It errors if the skill is already muted or
// has no description to strip.
func Mute(claudeDir, name, skillPath string) error {
	s, err := loadState(claudeDir)
	if err != nil {
		return err
	}
	if _, ok := s.Muted[name]; ok {
		return fmt.Errorf("already muted: %s", name)
	}
	b, err := os.ReadFile(skillPath)
	if err != nil {
		return err
	}
	stripped, ok := stripDescription(b)
	if !ok {
		return fmt.Errorf("no description to strip in %s", skillPath)
	}
	if err := os.MkdirAll(mutedDir(claudeDir), 0o755); err != nil {
		return err
	}
	backup := filepath.Join(mutedDir(claudeDir), sanitize(name)+".md.bak")
	if err := os.WriteFile(backup, b, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(skillPath, stripped, 0o644); err != nil {
		return err
	}
	s.Muted[name] = Entry{Path: skillPath, Backup: backup}
	return saveState(claudeDir, s)
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
	if err := restore(e); err != nil {
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
	n := 0
	for name, e := range s.Muted {
		if err := restore(e); err != nil {
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

func restore(e Entry) error {
	b, err := os.ReadFile(e.Backup)
	if err != nil {
		return err
	}
	if err := os.WriteFile(e.Path, b, 0o644); err != nil {
		return err
	}
	return os.Remove(e.Backup)
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
			if trimmed == "---" {
				inHeader = false
				out.WriteString(line)
				out.WriteByte('\n')
				continue
			}
			if strings.HasPrefix(trimmed, "description:") {
				found = true
				continue // drop the description line
			}
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes(), found
}
