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

// Path is where the keep list lives.
func Path(claudeDir string) string { return path(claudeDir) }

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

// RemoveKeep takes an item off the keep list and returns the key it removed. It
// accepts what keep accepts: the full key or the bare name, in any case. It works
// from the list itself, because an item no longer installed must still come off.
func RemoveKeep(claudeDir, arg string) (string, error) {
	f, err := load(path(claudeDir))
	if err != nil {
		return "", err
	}
	want := strings.ToLower(strings.TrimSpace(arg))
	var matches []string
	for _, k := range f.Keep {
		lk := strings.ToLower(k)
		if lk == want {
			matches = []string{k}
			break
		}
		// category:name, and the leaf of a namespaced name like skill:ecc:plan.
		if _, name, ok := strings.Cut(lk, ":"); ok && name == want {
			matches = append(matches, k)
		} else if i := strings.LastIndexByte(lk, ':'); i >= 0 && lk[i+1:] == want {
			matches = append(matches, k)
		}
	}
	switch len(matches) {
	case 0:
		if len(f.Keep) == 0 {
			return "", fmt.Errorf("not kept: %s (the keep list is empty)", arg)
		}
		return "", fmt.Errorf("not kept: %s. Kept items: %s", arg, strings.Join(f.Keep, ", "))
	case 1:
	default:
		return "", fmt.Errorf("%s matches %s; use the full key", arg, strings.Join(matches, ", "))
	}
	f.Keep = slices.DeleteFunc(slices.Clone(f.Keep), func(k string) bool { return k == matches[0] })
	return matches[0], save(path(claudeDir), f)
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
