package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// ReinvestPath is where reinvest leaves its last proposals, beside reap's own
// cache. Empty when the platform has no cache directory.
func ReinvestPath() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "skillreaper", "reinvest", "swaps.json")
}

// reinvestSwaps is the part of reinvest's swaps.json the hint needs. Command
// is written by reinvest, so the hint never names a command that is not there.
type reinvestSwaps struct {
	At      time.Time         `json:"at"`
	Command string            `json:"command"`
	Swaps   []json.RawMessage `json:"swaps"`
}

// RenderReinvestHint prints one line pointing at reinvest's last proposals.
// It prints nothing when there are none or the file is missing or unreadable:
// the hint is optional, the report is not. It only points; applying a swap
// stays a choice made inside reinvest.
func RenderReinvestHint(w io.Writer, path string, color bool) {
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var s reinvestSwaps
	if json.Unmarshal(b, &s) != nil || len(s.Swaps) == 0 || strings.TrimSpace(s.Command) == "" {
		return
	}
	noun := "swaps"
	if len(s.Swaps) == 1 {
		noun = "swap"
	}
	paint := painter(color)
	fmt.Fprintf(w, "  %s\n", paint(cDim, fmt.Sprintf("reinvest · %d %s ready (from %s)", len(s.Swaps), noun, s.At.Format("2006-01-02"))))
	fmt.Fprintf(w, "           %s\n", paint(cDim, fmt.Sprintf("run `%s` to review them", printable(s.Command))))
}

// printable drops control characters, so a cache file cannot move the cursor
// or recolour the terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
