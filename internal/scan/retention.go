package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultCleanupPeriodDays is Claude Code's retention window when
// cleanupPeriodDays is absent from settings. Transcripts older than this are
// deleted by a background sweep, so it is also the ceiling on how far back any
// evidence-based verdict can possibly reach.
const DefaultCleanupPeriodDays = 30

// settingsRetention mirrors only the retention key of settings.json. A pointer
// distinguishes "absent, so the default applies" from "explicitly set to this".
type settingsRetention struct {
	CleanupPeriodDays *int `json:"cleanupPeriodDays"`
}

// RetentionDays reports the transcript retention window configured for a
// Claude Code directory, and whether it was set explicitly. settings.local.json
// takes precedence over settings.json, matching Claude Code's own layering.
//
// An unreadable or malformed settings file is not an error here: the caller
// already surfaces those through the scanners that inventory the same files,
// and a second warning about the same file would be noise.
func RetentionDays(dir string) (days int, explicit bool) {
	days = DefaultCleanupPeriodDays
	// Lowest precedence first, so a later file overwrites an earlier one.
	for _, base := range []string{"settings.json", "settings.local.json"} {
		b, err := readCapped(filepath.Join(dir, base))
		if err != nil {
			if !os.IsNotExist(err) {
				continue
			}
			continue
		}
		var s settingsRetention
		if json.Unmarshal(b, &s) != nil || s.CleanupPeriodDays == nil {
			continue
		}
		// Claude Code rejects 0 as a validation error rather than treating it
		// as "never clean up", so a 0 on disk is a broken config, not infinite
		// retention. Leave the default standing rather than reporting a
		// horizon that does not exist.
		if *s.CleanupPeriodDays <= 0 {
			continue
		}
		days, explicit = *s.CleanupPeriodDays, true
	}
	return days, explicit
}
