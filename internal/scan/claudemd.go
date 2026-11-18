package scan

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
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
		paths = append(paths, memoryFilesUpFrom(cwd)...)
	}
	return paths
}

// memoryFilesUpFrom lists the memory files of a folder and of every folder
// above it: a session started there loads them all.
func memoryFilesUpFrom(start string) []string {
	var paths []string
	for dir := start; ; dir = filepath.Dir(dir) {
		paths = append(paths, filepath.Join(dir, "CLAUDE.md"), filepath.Join(dir, "CLAUDE.local.md"), filepath.Join(dir, ".claude", "CLAUDE.md"))
		if dir == filepath.Dir(dir) {
			return paths
		}
	}
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

// LoadClaudeMD reads every memory file a session loads and returns its lines,
// headings included. Missing files are skipped. The result feeds ClaudeMDReferences, which protects a
// referenced skill from REAP/MUTE.
func LoadClaudeMD(cwd, home, claudeDir string) []string {
	lines, _ := LoadClaudeMDChecked(cwd, home, claudeDir)
	return lines
}

// LoadClaudeMDChecked is LoadClaudeMD that also says which memory files are
// there and could not be read (no permission, a link leading nowhere, over
// the size limit). Such a file may name skills: read as missing, it stopped
// protecting them without a word.
//
// projects are the folders sessions ran in: their memory files are read too,
// wherever reap is run from. A skill named only in one project's CLAUDE.md
// was protected from inside that project and REAP from anywhere else.
func LoadClaudeMDChecked(cwd, home, claudeDir string, projects ...string) (lines []string, unread []Warning) {
	seen := map[string]bool{}
	paths := claudeMDPaths(cwd, home, claudeDir)
	for _, project := range projects {
		// As for the folder reap runs from: the project's rules, and the
		// memory files of every folder above it, which a session there loads
		// too (a repository's CLAUDE.md, for a session in one of its packages).
		paths = append(paths, markdownUnder(filepath.Join(project, ".claude", "rules"))...)
		paths = append(paths, memoryFilesUpFrom(project)...)
	}
	for _, p := range paths {
		lines = append(lines, readMemoryFile(p, home, seen, 0, &unread)...)
	}
	return lines, unread
}

// readMemoryFile returns one file's lines followed by those of its imports. seen
// keeps a file from being read twice, and so an import cycle from looping.
func readMemoryFile(path, home string, seen map[string]bool, depth int, unread *[]Warning) []string {
	if seen[path] || depth > maxImportDepth {
		return nil
	}
	seen[path] = true
	b, err := readCapped(path)
	if err != nil {
		// Not there is the usual case. There and unreadable, or a link whose
		// target is gone, is a file whose names reap cannot see.
		if _, statErr := os.Lstat(path); statErr == nil {
			*unread = append(*unread, Warning{Path: path, Msg: fmt.Sprintf("this memory file could not be read (%v), so the skills it names are not known: unused skills are shown as REVIEW, not REAP/MUTE", err)})
		}
		return nil
	}
	var lines []string
	sc := newLineScanner(trimBOM(b)) // or the mark hides what the first line begins with
	for sc.Scan() {
		line := sc.Text() // headings too: "#" opens one in Markdown, and the session loads it
		lines = append(lines, line)
		for _, m := range importRe.FindAllStringSubmatch(line, -1) {
			lines = append(lines, readMemoryFile(resolveImport(m[1], filepath.Dir(path), home), home, seen, depth+1, unread)...)
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
// "code-plan" or "ecc:plan", nor "обзор" by "обзорный". A letter is one in
// any alphabet.
func nameChar(r rune) bool {
	return r == '-' || r == '_' || r == ':' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r)
}

// endsWord reports whether a name ending at line[j] stops there. A colon closes
// it ("deploy: ship it") unless more of a name follows.
func endsWord(line string, j int) bool {
	if j == len(line) {
		return true
	}
	r, size := utf8.DecodeRuneInString(line[j:])
	if r == ':' {
		next, _ := utf8.DecodeRuneInString(line[j+size:])
		return j+size == len(line) || !nameChar(next)
	}
	return !nameChar(r)
}

// nameMarks are the pairs of marks a name is written between: code, emphasis
// (bold, and a bold label "**name:**"), a link's text, quotes.
var nameMarks = [][2]string{
	{"`", "`"}, {"<code>", "</code>"}, {"*", "*"}, {"*", ":*"}, {"[", "]("}, {"[", "]["},
	{`"`, `"`}, {"'", "'"}, {"“", "”"}, {"‘", "’"}, {"«", "»"},
}

const (
	// nearName is how far before a name a mark beside it can begin. The
	// patterns that end at the name read that much, not the whole line: a
	// long line that repeats the name took seconds.
	nearName = 64
	// longestHead is the longest start of a line that can be a list mark and
	// the names listed before this one.
	longestHead = 256
)

// listedBefore is the names listed before this one: "- review, deploy".
const listedBefore = `(?:[^\s,]+,\s*)*`

var (
	// commandBefore is the slash of a command, or of a path into a skills
	// folder: not the one inside "approve/review" or "docs/review".
	commandBefore = regexp.MustCompile("(?i)(?:^|[\\s(\"'`*\\[,;:]|skills)/$")
	// skillBefore and skillAfter are the word "skill" beside the name: English
	// puts it after, Italian before. "review skills" is a sentence.
	skillBefore = regexp.MustCompile(`(?i)\bskills?(?: named| called)?:? ` + listedBefore + `$`)
	skillAfter  = regexp.MustCompile(`(?i)^ skill\b`)
	// itemBefore is what stands before the first word of a list item (ticked
	// or numbered too), of a quoted line, or of a heading.
	itemBefore = regexp.MustCompile(`^\s*(?:(?:>\s*)+|(?:>\s*)*(?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?|#+\s+)` + listedBefore + `$`)
	// itemAfter is what follows a name that heads one: nothing, a colon, a
	// dash and its text, a comma and the next name, or a bracket.
	itemAfter = regexp.MustCompile(`^(?::|\s*$|\s+[-–—]\s|\s*[,(])`)
	// cellBefore and cellAfter are the bars of a table cell that is the name.
	cellBefore = regexp.MustCompile(`\|\s*$`)
	cellAfter  = regexp.MustCompile(`^\s*\|`)
)

// writtenAsName reports whether the word at line[i:j] is written as a name
// and not as a sentence's word: between marks, as a /command, beside the word
// "skill", heading a list item or a heading, or alone in a table cell.
func writtenAsName(line string, i, j int) bool {
	before, after := line[:i], line[j:]
	for _, mark := range nameMarks {
		if strings.HasSuffix(before, mark[0]) && strings.HasPrefix(after, mark[1]) {
			return true
		}
	}
	near := before // what ends at the name is near it
	if len(near) > nearName {
		near = near[len(near)-nearName:]
	}
	head := before // a list of names can run longer than a mark
	if len(head) > longestHead {
		head = head[len(head)-longestHead:]
	}
	return commandBefore.MatchString(near) || skillBefore.MatchString(head) || skillAfter.MatchString(after) ||
		len(before) <= longestHead && itemBefore.MatchString(before) && itemAfter.MatchString(after) ||
		cellBefore.MatchString(near) && cellAfter.MatchString(after)
}

// ClaudeMDReferences reports whether a skill is named in any of the given lines.
// A key with a dash, a colon, an underscore or a digit in it is no sentence's
// word, and counts wherever it stands alone. A key that is one plain word
// ("review"), like the bare suffix of a plugin key ("plan" of "ecc:plan"), is
// often an ordinary word: it has to be written as a name, or every rules file
// that says "always review the diff" protects a skill nobody uses.
func ClaudeMDReferences(lines []string, name string) bool {
	if name == "" {
		return false
	}
	var asName func(line string, i, j int) bool
	if plainWord(name) {
		asName = writtenAsName
	}
	_, bare, isPlugin := strings.Cut(name, ":")
	for _, line := range lines {
		if wordAt(line, name, asName) || isPlugin && bare != "" && wordAt(line, bare, writtenAsName) {
			return true
		}
	}
	return false
}

// ClaudeMDMentions reports whether the name stands alone in any line, written
// as a name or not: reap why says when it is there and protects nothing.
func ClaudeMDMentions(lines []string, name string) bool {
	if name == "" {
		return false
	}
	_, bare, isPlugin := strings.Cut(name, ":") // a plugin's skill goes by its short name too
	for _, line := range lines {
		if wordAt(line, name, nil) || isPlugin && bare != "" && wordAt(line, bare, nil) {
			return true
		}
	}
	return false
}

// plainWord says whether a name is letters and nothing else, in any alphabet.
func plainWord(name string) bool {
	for _, r := range name {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// wordAt reports whether word stands alone somewhere in line and, when a check
// is given, whether that occurrence passes it.
func wordAt(line, word string, check func(line string, i, j int) bool) bool {
	for from := 0; ; {
		k := indexFold(line[from:], word)
		if k < 0 {
			return false
		}
		i, j := from+k, from+k+len(word)
		last, _ := utf8.DecodeLastRuneInString(line[:i])
		alone := (i == 0 || !nameChar(last)) && endsWord(line, j)
		if alone && (check == nil || check(line, i, j)) {
			return true
		}
		from = i + 1
	}
}

// indexFold is strings.Index without regard to case: a sentence writes a
// skill's name in the case the sentence wants.
func indexFold(s, word string) int {
	for k := 0; k+len(word) <= len(s); k++ {
		if strings.EqualFold(s[k:k+len(word)], word) {
			return k
		}
	}
	return -1
}
