package override

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Keys use "category:name" format to disambiguate items that share a
// name across categories.

type File struct {
	Keep []string `json:"keep"`
}

func path(claudeDir string) string {
	return filepath.Join(claudeDir, "reaped", "overrides.json")
}

func AddKeep(claudeDir, key string) error {
	f, err := load(path(claudeDir))
	if err != nil {
		return err
	}
	for _, k := range f.Keep {
		if k == key {
			return nil
		}
	}
	f.Keep = append(f.Keep, key)
	return save(path(claudeDir), f)
}

func RemoveKeep(claudeDir, key string) error {
	f, err := load(path(claudeDir))
	if err != nil {
		return err
	}
	var updated []string
	for _, k := range f.Keep {
		if k != key {
			updated = append(updated, k)
		}
	}
	if len(updated) == len(f.Keep) {
		return fmt.Errorf("not found: %s", key)
	}
	f.Keep = updated
	return save(path(claudeDir), f)
}

func ListKeep(claudeDir string) ([]string, error) {
	f, err := load(path(claudeDir))
	if err != nil {
		return nil, err
	}
	return f.Keep, nil
}

func KeepSet(claudeDir string) (map[string]bool, error) {
	f, err := load(path(claudeDir))
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(f.Keep))
	for _, k := range f.Keep {
		set[k] = true
	}
	return set, nil
}

func ItemKey(category, name string) string {
	return strings.ToLower(category + ":" + name)
}

var mu sync.Mutex

func load(p string) (*File, error) {
	mu.Lock()
	defer mu.Unlock()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{}, nil
		}
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func save(p string, f *File) error {
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
