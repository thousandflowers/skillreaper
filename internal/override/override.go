package override

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/atomicfile"
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
	if slices.Contains(f.Keep, key) {
		return nil
	}
	f.Keep = append(f.Keep, key)
	return save(path(claudeDir), f)
}

func RemoveKeep(claudeDir, key string) error {
	f, err := load(path(claudeDir))
	if err != nil {
		return err
	}
	if !slices.Contains(f.Keep, key) {
		return fmt.Errorf("not found: %s", key)
	}
	f.Keep = slices.DeleteFunc(slices.Clone(f.Keep), func(k string) bool { return k == key })
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

func load(p string) (File, error) {
	return atomicfile.LoadJSON[File](p)
}

func save(p string, f File) error {
	return atomicfile.SaveJSON(p, f)
}
