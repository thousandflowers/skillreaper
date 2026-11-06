// Package hook installs and removes skillreaper's SessionStart nudge hook in
// settings.json, and tracks the state that throttles the nudge to weekly.
package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/atomicfile"
	"github.com/thousandflowers/skillreaper/internal/jsonedit"
)

// defaultSettingsMode is for a settings.json this tool creates.
const defaultSettingsMode = 0o644

// starCtaCooldownDays limits the star-CTA to at most once per 30 days.
const starCtaCooldownDays = 30

// maxStarCtaShowings caps the star-CTA for the life of the install. The
// cooldown alone only spaces the asks out; without a ceiling a user who keeps
// the tool for years keeps being asked forever, and an ask that never stops is
// not a nudge. Three is enough to be seen and few enough to be forgettable.
const maxStarCtaShowings = 3

// shareHintCooldownDays limits the share-command hint to at most once per 30 days.
const shareHintCooldownDays = 30

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
	return shellQuote(exe) + " nudge  # " + Marker
}

// shellQuote wraps s in single quotes so a path with spaces or shell
// metacharacters is passed to the shell as a single, literal argument.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// isOurHook reports whether a hook command is skillreaper's nudge entry. The
// marker rides as a trailing shell comment, so match it as a suffix rather
// than a substring — a foreign command that merely mentions the marker text
// must not be treated as ours.
func isOurHook(command string) bool {
	return strings.HasSuffix(strings.TrimSpace(command), "# "+Marker)
}

// sessionStartPath is where the hook groups sit in settings.json.
var sessionStartPath = []string{"hooks", "SessionStart"}

// Install adds the SessionStart nudge hook to settings.json (creating the file
// if absent). Nothing else in the file changes: not another key, not a hook's
// other fields, not the file's mode. It is idempotent. With dryRun the resulting
// JSON is returned without writing.
func Install(settingsPath, command string, dryRun bool) ([]byte, error) {
	doc, perm, err := readSettings(settingsPath)
	if err != nil {
		return nil, err
	}
	groups, err := sessionStartGroups(doc)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		for _, h := range g.Hooks {
			if isOurHook(h.Command) {
				return doc, nil // already installed
			}
		}
	}
	entry, err := marshalNoEscape(group{Hooks: []cmdEntry{{Type: "command", Command: command}}})
	if err != nil {
		return nil, err
	}
	out, err := jsonedit.Append(doc, entry, sessionStartPath...)
	if err != nil {
		return nil, fmt.Errorf("unreadable settings %s: %w", settingsPath, err)
	}
	if dryRun {
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return nil, err
	}
	if err := writeSettings(settingsPath, out, perm); err != nil {
		return nil, err
	}
	return out, nil
}

// Uninstall removes only skillreaper's nudge entry from SessionStart, and the
// containers that entry alone was keeping alive, so an install followed by an
// uninstall leaves the file as it was. Missing files and missing entries are
// no-ops.
func Uninstall(settingsPath string) error {
	if _, err := os.Stat(settingsPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	doc, perm, err := readSettings(settingsPath)
	if err != nil {
		return err
	}
	groups, err := sessionStartGroups(doc)
	if err != nil {
		return err
	}
	out := doc
	// Backwards, so the indexes still to come stay valid as elements go.
	for i := len(groups) - 1; i >= 0; i-- {
		kept := len(groups[i].Hooks)
		for j := len(groups[i].Hooks) - 1; j >= 0; j-- {
			if !isOurHook(groups[i].Hooks[j].Command) {
				continue
			}
			kept--
			path := append(slices.Clone(sessionStartPath), strconv.Itoa(i), "hooks", strconv.Itoa(j))
			if out, _, err = jsonedit.Delete(out, path...); err != nil {
				return err
			}
		}
		if kept == 0 {
			if out, _, err = jsonedit.Delete(out, append(slices.Clone(sessionStartPath), strconv.Itoa(i))...); err != nil {
				return err
			}
		}
	}
	if bytes.Equal(out, doc) {
		return nil
	}
	if out, err = dropIfEmpty(out, sessionStartPath...); err != nil {
		return err
	}
	if out, err = dropIfEmpty(out, sessionStartPath[:1]...); err != nil {
		return err
	}
	return writeSettings(settingsPath, out, perm)
}

// writeSettings writes through a link: settings.json is often a link into a
// dotfiles repo, and a rename onto the link would cut it off from there.
func writeSettings(path string, b []byte, perm os.FileMode) error {
	target := path
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if target, err = filepath.EvalSymlinks(path); err != nil {
			return fmt.Errorf("%s is a link that leads nowhere: %w", path, err)
		}
	}
	return atomicfile.Write(target, b, perm)
}

// dropIfEmpty removes the container at path when nothing is left in it.
func dropIfEmpty(doc []byte, path ...string) ([]byte, error) {
	raw, err := jsonedit.Get(doc, path...)
	if errors.Is(err, jsonedit.ErrNotFound) {
		return doc, nil
	}
	if err != nil {
		return nil, err
	}
	if s := strings.TrimSpace(string(raw[1 : len(raw)-1])); s != "" {
		return doc, nil
	}
	out, _, err := jsonedit.Delete(doc, path...)
	return out, err
}

// readSettings returns the file and the mode to write it back with. A file that
// holds a secret stays as private as it is; only a new file gets the default.
func readSettings(path string) ([]byte, os.FileMode, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []byte("{}\n"), defaultSettingsMode, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, 0, fmt.Errorf("unreadable settings %s: %w", path, err)
	}
	if top == nil { // the file says null
		return nil, 0, fmt.Errorf("unreadable settings %s: not a JSON object", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	return b, info.Mode().Perm(), nil
}

// sessionStartGroups decodes just enough of the hooks to find commands.
func sessionStartGroups(doc []byte) ([]group, error) {
	raw, err := jsonedit.Get(doc, sessionStartPath...)
	if errors.Is(err, jsonedit.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var groups []group
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// marshalNoEscape encodes v leaving & < > readable, as a person wrote them.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}

// NudgeState is persisted between sessions to throttle the nudge to weekly
// and the star-CTA to at-most-monthly.
type NudgeState struct {
	LastNudgeAt   time.Time `json:"last_nudge_at"`
	LastReapCount int       `json:"last_reap_count"`
	LastMuteCount int       `json:"last_mute_count"`

	// LastStarCtaAt is when the star-CTA was last shown. Zero = never shown.
	LastStarCtaAt time.Time `json:"last_star_cta_at,omitempty"`
	// StarCtaCount tracks how many times it has been shown across months.
	StarCtaCount int `json:"star_cta_count,omitempty"`

	// LastShareHintAt is when the share-command hint was last shown. Zero = never shown.
	LastShareHintAt time.Time `json:"last_share_hint_at,omitempty"`
	// ShareHintCount tracks how many times the share hint has been shown.
	ShareHintCount int `json:"share_hint_count,omitempty"`
}

func nudgeStatePath(claudeDir string) string {
	return filepath.Join(claudeDir, "reaped", "nudge-state.json")
}

// LoadNudgeState returns the saved nudge state, or a zero state when none
// exists yet (which makes the first run eligible to nudge).
func LoadNudgeState(claudeDir string) (NudgeState, error) {
	return atomicfile.LoadJSON[NudgeState](nudgeStatePath(claudeDir))
}

// SaveNudgeState writes the nudge state, creating the reaped/ directory.
func SaveNudgeState(claudeDir string, s NudgeState) error {
	return atomicfile.SaveJSON(nudgeStatePath(claudeDir), s)
}

// ShouldNudge reports whether a passive nudge should print now: at least
// nudgeIntervalDays since the last nudge, and the reap or mute count has grown.
func ShouldNudge(now time.Time, reapCount, muteCount int, st NudgeState) bool {
	if now.Sub(st.LastNudgeAt) < nudgeIntervalDays*24*time.Hour {
		return false
	}
	return reapCount > st.LastReapCount || muteCount > st.LastMuteCount
}

// ShouldShowStarCta reports whether the star-CTA should be shown now. Two
// limits apply, and the lifetime one is checked first: after
// maxStarCtaShowings it never returns true again, no matter how much time
// passes. Under that ceiling it appears at most once per starCtaCooldownDays.
// The count is read from state written since the field existed, so the cap
// applies to installs that have already been asked rather than restarting
// their tally at zero.
func ShouldShowStarCta(now time.Time, st NudgeState) bool {
	if st.StarCtaCount >= maxStarCtaShowings {
		return false // asked enough, for good
	}
	if st.LastStarCtaAt.IsZero() {
		return true // never shown
	}
	return now.Sub(st.LastStarCtaAt) >= starCtaCooldownDays*24*time.Hour
}

// ShouldShowShareHint reports whether the share-command hint should be
// shown now. It follows the same throttle as the star CTA: at most once
// unless at least shareHintCooldownDays have passed.
func ShouldShowShareHint(now time.Time, st NudgeState) bool {
	if st.LastShareHintAt.IsZero() {
		return true // never shown
	}
	return now.Sub(st.LastShareHintAt) >= shareHintCooldownDays*24*time.Hour
}
