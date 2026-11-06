// Package atomicfile writes a file atomically: the content goes to a temporary
// file in the same directory which is then renamed into place. A reader never
// sees a half-written file, and a crash mid-write leaves either the complete
// old file or the complete new one — never a truncated mix. The target's
// directory must already exist.
package atomicfile

import (
	"encoding/json"
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

// LoadJSON decodes the JSON file at path into a T. A missing file is the zero
// T, not an error: state files start out absent.
func LoadJSON[T any](path string) (T, error) {
	var v T
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return v, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &v)
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return v, nil
}

// SaveJSON writes v as indented JSON to path atomically, creating its
// directory, readable by the owner only.
func SaveJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return Write(path, b, 0o600)
}
