// Package safepath holds path-confinement helpers shared by packages that
// move or rewrite files inside the Claude directory (prune, mute) and the
// scanner that follows symlinks. Centralizing these keeps the security
// boundary identical everywhere instead of drifting between copies.
package safepath

import (
	"path/filepath"
	"strings"
)

// WithinDir reports whether target is at or under root after both are made
// absolute. It does not resolve symlinks itself; callers that need to defend
// against symlink redirection should EvalSymlinks first and pass the resolved
// paths in.
func WithinDir(root, target string) bool {
	ra, err1 := filepath.Abs(root)
	ta, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(ra, ta)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// Sanitize rewrites characters that are unsafe in a path segment (path
// separators and the Windows drive separator) into dashes, so an item name
// can be used as a single filename without escaping its directory.
func Sanitize(name string) string {
	return strings.NewReplacer(":", "-", "/", "-", "\\", "-").Replace(name)
}
