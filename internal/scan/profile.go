package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// ProfileKey names the folder that holds what skillreaper keeps outside a
// Claude config folder but about it: reinvest's memory of sessions, its groups,
// its proposals. The usual folder (home/.claude, or none named) has the empty
// key, so what was kept before profiles were told apart is still found. Any
// other folder gets a key of its own: a second profile, a fixture or a test
// must not be read as the first, nor have its prompts sent on its behalf.
func ProfileKey(claudeDir, home string) string {
	if claudeDir == "" {
		return ""
	}
	dir := filepath.Clean(claudeDir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if home != "" && dir == filepath.Join(home, ".claude") {
		return ""
	}
	sum := sha256.Sum256([]byte(dir))
	return "profile-" + hex.EncodeToString(sum[:8])
}
