// Package hook installs and removes skillreaper's SessionStart nudge hook in
// settings.json, and tracks the state that throttles the nudge to weekly.
package hook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Marker identifies skillreaper's hook command so Uninstall can find it
// without disturbing other hooks. It rides as a shell comment, ignored at run.
const Marker = "skillreaper-weekly-nudge"

const nudgeIntervalDays = 7

// cmdEntry shapes one hook command entry in settings.json.
type cmdEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// group is one matcher group under a hook event.
type group struct {
	Matcher string     `json:"matcher,omitempty"`
	Hooks   []cmdEntry `json:"hooks"`
}

// Command builds the hook command string for the given reap executable path.
// Running `reap nudge` performs the audit and weekly comparison internally,
// so the hook needs no external dependencies.
func Command(exe string) string {
	return exe + " nudge  # " + Marker
}

// Install adds the SessionStart nudge hook to settings.json (creating the file
// if absent), preserving existing hooks and top-level keys. It is idempotent.
// With dryRun the resulting JSON is returned without writing.
func Install(settingsPath, command string, dryRun bool) ([]byte, error) {
	top, err := readTop(settingsPath)
	if err != nil {
		return nil, err
	}
	hooks, err := decodeHooks(top["hooks"])
	if err != nil {
		return nil, err
	}
	for _, g := range hooks["SessionStart"] {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, Marker) {
				return marshalTop(top) // already installed
			}
		}
	}
	hooks["SessionStart"] = append(hooks["SessionStart"], group{
		Hooks: []cmdEntry{{Type: "command", Command: command}},
	})
	if err := encodeHooks(top, hooks); err != nil {
		return nil, err
	}
	out, err := marshalTop(top)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return nil, err
	}
	return out, nil
}

// Uninstall removes only skillreaper's nudge entry from SessionStart, leaving
// other hooks intact. Missing files and missing entries are no-ops.
func Uninstall(settingsPath string) error {
	if _, err := os.Stat(settingsPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	top, err := readTop(settingsPath)
	if err != nil {
		return err
	}
	hooks, err := decodeHooks(top["hooks"])
	if err != nil {
		return err
	}
	var kept []group
	for _, g := range hooks["SessionStart"] {
		var kc []cmdEntry
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, Marker) {
				continue
			}
			kc = append(kc, h)
		}
		if len(kc) == 0 {
			continue // drop a group left empty
		}
		g.Hooks = kc
		kept = append(kept, g)
	}
	if len(kept) == 0 {
		delete(hooks, "SessionStart")
	} else {
		hooks["SessionStart"] = kept
	}
	if err := encodeHooks(top, hooks); err != nil {
		return err
	}
	out, err := marshalTop(top)
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0o644)
}

func readTop(path string) (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]json.RawMessage{}, nil
		}
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, fmt.Errorf("unreadable settings %s: %w", path, err)
	}
	if top == nil {
		top = map[string]json.RawMessage{}
	}
	return top, nil
}

func decodeHooks(raw json.RawMessage) (map[string][]group, error) {
	if len(raw) == 0 {
		return map[string][]group{}, nil
	}
	var h map[string][]group
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	if h == nil {
		h = map[string][]group{}
	}
	return h, nil
}

func encodeHooks(top map[string]json.RawMessage, hooks map[string][]group) error {
	if len(hooks) == 0 {
		delete(top, "hooks")
		return nil
	}
	b, err := json.Marshal(hooks)
	if err != nil {
		return err
	}
	top["hooks"] = b
	return nil
}

func marshalTop(top map[string]json.RawMessage) ([]byte, error) {
	return json.MarshalIndent(top, "", "  ")
}

// NudgeState is persisted between sessions to throttle the nudge to weekly.
type NudgeState struct {
	LastNudgeAt   time.Time `json:"last_nudge_at"`
	LastReapCount int       `json:"last_reap_count"`
	LastMuteCount int       `json:"last_mute_count"`
}

func nudgeStatePath(claudeDir string) string {
	return filepath.Join(claudeDir, "reaped", "nudge-state.json")
}

// LoadNudgeState returns the saved nudge state, or a zero state when none
// exists yet (which makes the first run eligible to nudge).
func LoadNudgeState(claudeDir string) (NudgeState, error) {
	b, err := os.ReadFile(nudgeStatePath(claudeDir))
	if err != nil {
		if os.IsNotExist(err) {
			return NudgeState{}, nil
		}
		return NudgeState{}, err
	}
	var s NudgeState
	if err := json.Unmarshal(b, &s); err != nil {
		return NudgeState{}, err
	}
	return s, nil
}

// SaveNudgeState writes the nudge state, creating the reaped/ directory.
func SaveNudgeState(claudeDir string, s NudgeState) error {
	if err := os.MkdirAll(filepath.Dir(nudgeStatePath(claudeDir)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(nudgeStatePath(claudeDir), b, 0o644)
}

// ShouldNudge reports whether a passive nudge should print now: at least
// nudgeIntervalDays since the last nudge, and the reap or mute count has grown.
func ShouldNudge(now time.Time, reapCount, muteCount int, st NudgeState) bool {
	if now.Sub(st.LastNudgeAt) < nudgeIntervalDays*24*time.Hour {
		return false
	}
	return reapCount > st.LastReapCount || muteCount > st.LastMuteCount
}
