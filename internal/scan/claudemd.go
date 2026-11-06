package scan

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

// maxImportDepth is how many @imports Claude Code follows from a memory file.
const maxImportDepth = 5

// importRe finds an @path in a memory file; one that is no file reads as nothing.
var importRe = regexp.MustCompile(`(?:^|\s)@([~\w./-]+)`)

// claudeMDPaths lists the memory files a session reads: the user's, then those
// of the project from the working directory up. Any argument may be empty.
func claudeMDPaths(cwd, home, claudeDir string) []string {
	var paths []string
	if claudeDir != "" {
		paths = append(paths, filepath.Join(claudeDir, "CLAUDE.md"))
		paths = append(paths, markdownUnder(filepath.Join(claudeDir, "rules"))...)
	}
	if home != "" {
		paths = append(paths, filepath.Join(home, "CLAUDE.md"))
	}
	if cwd != "" {
		paths = append(paths, markdownUnder(filepath.Join(cwd, ".claude", "rules"))...)
		for dir := cwd; ; dir = filepath.Dir(dir) {
			paths = append(paths, filepath.Join(dir, "CLAUDE.md"), filepath.Join(dir, ".claude", "CLAUDE.md"))
			if dir == filepath.Dir(dir) {
				break
			}
		}
	}
	return paths
}

// markdownUnder lists the .md files in a rules folder, however deep, which may
// itself be a link. A missing folder lists nothing.
func markdownUnder(root string) []string {
	// WalkDir does not follow a link at its root.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	var paths []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) == ".md" {
			paths = append(paths, p)
		}
		return nil
	})
	return paths
}

// LoadClaudeMD reads every memory file a session loads and returns the
// non-comment lines (those that do not start with "#" after trimming). Missing
// files are skipped. The result feeds ClaudeMDReferences, which protects a
// referenced skill from REAP/MUTE.
func LoadClaudeMD(cwd, home, claudeDir string) []string {
	var lines []string
	seen := map[string]bool{}
	for _, p := range claudeMDPaths(cwd, home, claudeDir) {
		lines = append(lines, readMemoryFile(p, home, seen, 0)...)
	}
	return lines
}

// readMemoryFile returns one file's lines followed by those of its imports. seen
// keeps a file from being read twice, and so an import cycle from looping.
func readMemoryFile(path, home string, seen map[string]bool, depth int) []string {
	if seen[path] || depth > maxImportDepth {
		return nil
	}
	seen[path] = true
	b, err := readCapped(path)
	if err != nil {
		return nil
	}
	var lines []string
	sc := newLineScanner(b)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines = append(lines, line)
		for _, m := range importRe.FindAllStringSubmatch(line, -1) {
			lines = append(lines, readMemoryFile(resolveImport(m[1], filepath.Dir(path), home), home, seen, depth+1)...)
		}
	}
	return lines
}

func resolveImport(ref, dir, home string) string {
	if rest, ok := strings.CutPrefix(ref, "~/"); ok {
		return filepath.Join(home, rest)
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(dir, ref)
}

// nameChar is what can extend a name: "plan" is not mentioned by "planner",
// "code-plan" or "ecc:plan".
func nameChar(c byte) bool {
	return c == '-' || c == '_' || c == ':' ||
		c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// endsWord reports whether a name ending at line[j] stops there. A colon closes
// it ("deploy: ship it") unless more of a name follows.
func endsWord(line string, j int) bool {
	if j == len(line) {
		return true
	}
	if line[j] == ':' {
		return j+1 == len(line) || !nameChar(line[j+1])
	}
	return !nameChar(line[j])
}

// markedAsSkill reports whether the word at line[i:j] is written as a skill: in
// backticks or bold, as a /command, or next to the word "skill" on either side:
// English puts it after the name, Italian before.
func markedAsSkill(line string, i, j int) bool {
	before, after := line[:i], line[j:]
	return strings.HasSuffix(before, "`") && strings.HasPrefix(after, "`") ||
		strings.HasSuffix(before, "**") && strings.HasPrefix(after, "**") ||
		strings.HasSuffix(before, "/") ||
		strings.HasPrefix(after, " skill") ||
		strings.HasSuffix(strings.ToLower(before), "skill ")
}

// ClaudeMDReferences reports whether a skill is named in any of the given lines.
// Its whole invocation key counts as a word. The bare suffix of a plugin key
// ("plan" of "ecc:plan") has to be marked as a skill, because it is often an
// ordinary word and every rules file would otherwise protect it.
func ClaudeMDReferences(lines []string, name string) bool {
	if name == "" {
		return false
	}
	_, bare, isPlugin := strings.Cut(name, ":")
	for _, line := range lines {
		if wordAt(line, name, nil) || isPlugin && bare != "" && wordAt(line, bare, markedAsSkill) {
			return true
		}
	}
	return false
}

// wordAt reports whether word stands alone somewhere in line and, when a check
// is given, whether that occurrence passes it.
func wordAt(line, word string, check func(line string, i, j int) bool) bool {
	for from := 0; ; {
		k := strings.Index(line[from:], word)
		if k < 0 {
			return false
		}
		i, j := from+k, from+k+len(word)
		alone := (i == 0 || !nameChar(line[i-1])) && endsWord(line, j)
		if alone && (check == nil || check(line, i, j)) {
			return true
		}
		from = i + 1
	}
}
