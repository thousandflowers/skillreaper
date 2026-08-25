package usage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cacheFormat is bumped whenever the serialised shape of Stats changes.
// A mismatch is a miss, never a partial read: an old cache decoded into a new
// struct would silently drop whatever field was added.
const cacheFormat = 1

// Fingerprint identifies a corpus and the window applied to it, cheaply enough
// to be worth checking before parsing.
//
// Measured on an 800-transcript, 345 MB corpus: walking and stat-ing the tree
// takes 22 ms, reading every byte takes 0.33 s, and a full run takes about
// five seconds. The cost is JSON parsing, not I/O — so a fingerprint built
// from stat alone buys back nearly all of it.
//
// Path, size and modification time of every transcript are folded in, along
// with the window and the format version. Transcripts are append-only and a
// new session is a new file, so any change to the evidence changes the
// fingerprint. An empty string means "do not cache": callers must parse.
func Fingerprint(dirs []string, windowDays int) string {
	type ent struct {
		path string
		size int64
		mod  int64
	}
	var ents []ent
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			ents = append(ents, ent{path, info.Size(), info.ModTime().UnixNano()})
			return nil
		})
		if err != nil {
			// A tree that cannot be walked completely must not produce a
			// fingerprint: caching a partial corpus under a name that looks
			// complete is worse than never caching at all.
			return ""
		}
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].path < ents[j].path })
	h := sha256.New()
	fmt.Fprintf(h, "v%d;days=%d;", cacheFormat, windowDays)
	for _, e := range ents {
		fmt.Fprintf(h, "%s|%d|%d;", e.path, e.size, e.mod)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// cacheEnvelope wraps the stats so a mismatched fingerprint is detected before
// the payload is trusted.
type cacheEnvelope struct {
	Format      int    `json:"format"`
	Fingerprint string `json:"fingerprint"`
	Stats       *Stats `json:"stats"`
}

// CachePath is where a corpus cache lives for a given Claude directory.
//
// Deliberately outside the Claude directory. `reap` is documented as
// read-only, and that claim is a large part of why people run it at all; a
// cache dropped into ~/.claude would quietly break it, and would sit in the
// same tree the scanners inventory. It goes in the user cache directory
// instead, named by a hash of the Claude directory so several of them do not
// collide. When no cache directory is available, caching is simply off.
func CachePath(claudeDir string) string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(claudeDir))
	return filepath.Join(base, "skillreaper", "corpus-"+hex.EncodeToString(sum[:8])+".json")
}

// LoadCache returns the cached stats for this fingerprint. Any problem at all
// — missing file, unreadable, wrong format, wrong fingerprint, malformed JSON —
// is a miss, because a wrong cache produces wrong verdicts silently and
// reparsing costs only seconds.
func LoadCache(claudeDir, fingerprint string) (*Stats, bool) {
	if fingerprint == "" {
		return nil, false
	}
	path := CachePath(claudeDir)
	if path == "" {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var env cacheEnvelope
	if json.Unmarshal(b, &env) != nil {
		return nil, false
	}
	if env.Format != cacheFormat || env.Fingerprint != fingerprint || env.Stats == nil {
		return nil, false
	}
	return env.Stats, true
}

// SaveCache stores stats under this fingerprint. Failures are silent: a cache
// that cannot be written is a missed optimisation, not an error the user needs
// to hear about while reading a report.
func SaveCache(claudeDir, fingerprint string, st *Stats) {
	if fingerprint == "" || st == nil {
		return
	}
	path := CachePath(claudeDir)
	if path == "" {
		return
	}
	b, err := json.Marshal(cacheEnvelope{Format: cacheFormat, Fingerprint: fingerprint, Stats: st})
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		os.Remove(tmp)
	}
}
