package scan

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// claudeMDPaths lists the CLAUDE.md locations to scan, in priority order:
// ./CLAUDE.md, ~/CLAUDE.md, ./.claude/CLAUDE.md. cwd or home may be empty.
func claudeMDPaths(cwd, home string) []string {
	var paths []string
	if cwd != "" {
		paths = append(paths, filepath.Join(cwd, "CLAUDE.md"))
	}
	if home != "" {
		paths = append(paths, filepath.Join(home, "CLAUDE.md"))
	}
	if cwd != "" {
		paths = append(paths, filepath.Join(cwd, ".claude", "CLAUDE.md"))
	}
	return paths
}

// LoadClaudeMD reads every existing CLAUDE.md location and returns the
// non-comment lines (those that do not start with "#" after trimming).
// Missing files are skipped. The result feeds ClaudeMDReferences, which
// protects a referenced skill from REAP/MUTE.
func LoadClaudeMD(cwd, home string) []string {
	var lines []string
	for _, p := range claudeMDPaths(cwd, home) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			lines = append(lines, line)
		}
	}
	return lines
}

// ClaudeMDReferences reports whether name (a skill's invocation key) or its
// bare suffix appears as a substring in any of the given CLAUDE.md lines.
// For a plugin skill "ecc:plan" both "ecc:plan" and "plan" are checked.
func ClaudeMDReferences(lines []string, name string) bool {
	if name == "" {
		return false
	}
	bare := name
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		bare = name[i+1:]
	}
	for _, line := range lines {
		if strings.Contains(line, name) || (bare != "" && strings.Contains(line, bare)) {
			return true
		}
	}
	return false
}
