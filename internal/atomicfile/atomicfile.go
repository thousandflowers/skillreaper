// Package atomicfile writes a file atomically: the content goes to a temporary
// file in the same directory which is then renamed into place. A reader never
// sees a half-written file, and a crash mid-write leaves either the complete
// old file or the complete new one — never a truncated mix. The target's
// directory must already exist.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write writes data to path atomically with the given permissions. It also
// fsyncs the file and its parent directory so the new entry survives a crash
// rather than being held only in memory.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Removed after a successful rename is a no-op; on any error it cleans up.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	// Durably commit the directory entry so the rename is not lost on crash.
	// Best-effort: some platforms/filesystems cannot fsync a directory, in
	// which case the data fsync above still guarantees the file's contents.
	syncDir(dir)
	return nil
}

func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	defer d.Close()
	_ = d.Sync()
}
