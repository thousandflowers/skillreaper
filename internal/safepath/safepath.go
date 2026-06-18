// Package safepath holds path-confinement helpers shared by packages that
// move or rewrite files inside the Claude directory (prune, mute) and the
// scanner that follows symlinks. Centralizing these keeps the security
// boundary identical everywhere instead of drifting between copies.
package safepath

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/atomicfile"
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

// ExistingPathWithin resolves root and target and returns the resolved target
// only when it remains under root. It follows symlinks so callers can reject
// symlink escapes even when the lexical path appears safe.
func ExistingPathWithin(root, target string) (string, error) {
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	tr, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", err
	}
	if !WithinDir(rr, tr) {
		return "", fmt.Errorf("refusing to use path outside %s: %s", root, target)
	}
	return tr, nil
}

// ExistingRegularFileWithin is ExistingPathWithin plus a final-path lstat
// check. The final path itself must be a regular file, not a symlink, FIFO,
// directory, or device.
func ExistingRegularFileWithin(root, target string) (string, error) {
	info, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to use symlink %s", target)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to use non-regular file %s", target)
	}
	return ExistingPathWithin(root, target)
}

// ReadRegularFileWithin reads an existing regular file under root with a hard
// byte cap. It refuses final symlinks and rechecks the opened file so special
// files cannot block or stream unbounded data.
func ReadRegularFileWithin(root, target string, maxBytes int64) ([]byte, error) {
	resolved, err := ExistingRegularFileWithin(root, target)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing to use non-regular file %s", target)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%s is %d bytes, over the %d-byte read limit", target, info.Size(), maxBytes)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("%s is over the %d-byte read limit", target, maxBytes)
	}
	return b, nil
}

func nearestExistingAncestor(path string) (string, error) {
	cur := filepath.Clean(path)
	for {
		if _, err := os.Lstat(cur); err == nil {
			return cur, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		next := filepath.Dir(cur)
		if next == cur {
			return "", fmt.Errorf("no existing ancestor for %s", path)
		}
		cur = next
	}
}

// ParentWithinForWrite creates target's parent only when the lexical target is
// under root and every existing/resolved ancestor stays under root. It refuses
// writes through symlinked parents that escape the intended tree.
func ParentWithinForWrite(root, target string) error {
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if !WithinDir(absRoot, absTarget) {
		return fmt.Errorf("refusing to write outside %s: %s", root, target)
	}

	parent := filepath.Dir(absTarget)
	existing, err := nearestExistingAncestor(parent)
	if err != nil {
		return err
	}
	er, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	if !WithinDir(rr, er) {
		return fmt.Errorf("refusing to write through path outside %s: %s", root, target)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	pr, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return err
	}
	if !WithinDir(rr, pr) {
		return fmt.Errorf("refusing to write through path outside %s: %s", root, target)
	}
	return nil
}

// RejectFinalSymlink refuses a final path symlink while allowing the path not
// to exist yet.
func RejectFinalSymlink(path string) error {
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through symlink %s", path)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AtomicWriteFileWithin writes target atomically after validating the parent
// path stays under root and the final path is not a symlink.
func AtomicWriteFileWithin(root, target string, b []byte, perm os.FileMode) error {
	if err := ParentWithinForWrite(root, target); err != nil {
		return err
	}
	if err := RejectFinalSymlink(target); err != nil {
		return err
	}
	return atomicfile.Write(target, b, perm)
}
