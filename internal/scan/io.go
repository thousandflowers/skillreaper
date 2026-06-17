package scan

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// maxFileSize caps how large a config or markdown file the scanner reads into
// memory. The line scanners only bound per-line size, so a crafted
// multi-gigabyte file under the scan root would otherwise be read whole and
// could OOM the process. 10 MiB is far above any real skill/agent/config file.
const maxFileSize = 10 << 20

// readCapped reads path but refuses files larger than maxFileSize. Callers
// treat the error like any other read failure (skip the file, optionally warn).
func readCapped(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Size() > maxFileSize {
		return nil, fmt.Errorf("%s is %d bytes, over the %d-byte scan limit", path, fi.Size(), maxFileSize)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Size() > maxFileSize {
		return nil, fmt.Errorf("%s is %d bytes, over the %d-byte scan limit", path, fi.Size(), maxFileSize)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFileSize {
		return nil, fmt.Errorf("%s is over the %d-byte scan limit", path, maxFileSize)
	}
	return b, nil
}

func resolveWithin(root, target string) (string, bool) {
	rr, err1 := filepath.EvalSymlinks(root)
	tr, err2 := filepath.EvalSymlinks(target)
	if err1 != nil || err2 != nil {
		return "", false
	}
	if !withinDir(rr, tr) {
		return "", false
	}
	return tr, true
}

// withinDir reports whether target is at or under root. Both should already be
// symlink-resolved by the caller when that matters.
func withinDir(root, target string) bool {
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
