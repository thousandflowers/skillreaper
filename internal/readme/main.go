// Command readme rewrites the figures in README.md that are measured on the
// maintainer's own install, from one `reap --json` run handed to it on stdin.
//
// It exists because those numbers cannot come from the sample stack: they are
// the whole point of the claim ("measured on mine"), and no contributor can
// reproduce them. Everything here derives from a single decoded report, so the
// page can never again carry two figures from two different runs - which is
// exactly how it drifted before.
//
// The blocks it owns are marked readme-mine-*. It never touches the readme-*
// blocks that `make readme-numbers` generates from the fixture, and that target
// never touches these, so running the wrong one cannot overwrite the other's
// numbers.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/report"
)

func main() {
	if err := run(os.Args[1:], os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "readme: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdin *os.File) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: reap --json | go run ./internal/readme README.md")
	}
	path := args[0]

	var r report.Report
	if err := json.NewDecoder(stdin).Decode(&r); err != nil {
		return fmt.Errorf("decode reap --json from stdin: %w", err)
	}
	// Fail closed on a report that would render nonsense rather than writing
	// zeroes over real measurements.
	if r.Gap == nil || r.Gap.Loaded == 0 || r.Sessions == 0 || r.DeadTokensPerSession == 0 {
		return fmt.Errorf("report has no usable measurements (loaded=%d sessions=%d dead-tok=%d)",
			gapLoaded(&r), r.Sessions, r.DeadTokensPerSession)
	}

	readme, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := string(readme)
	for _, b := range blocks(&r) {
		out, err = replace(out, b.marker, b.body)
		if err != nil {
			return err
		}
	}
	if out == string(readme) {
		return nil // nothing moved; leave the file's mtime alone
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

func gapLoaded(r *report.Report) int {
	if r.Gap == nil {
		return 0
	}
	return r.Gap.Loaded
}

type block struct {
	marker string
	body   string
}

// blocks renders every marked region from one report. The derivations are the
// ones the page already used: kept is what survives a prune, monthly tokens are
// the per-session figure times the extrapolated session count, and the price is
// recovered from the money field rather than restated, so the arithmetic shown
// is the arithmetic the tool did.
func blocks(r *report.Report) []block {
	loaded, fired := r.Gap.Loaded, r.Gap.Fired
	dead := r.DeadCount
	tok := r.DeadTokensPerSession
	monthly := tok * r.SessionsPerMonth
	kept := loaded - dead
	price := 0.0
	if tok > 0 && r.SessionsPerMonth > 0 {
		price = r.MoneyPerMonth * 1e6 / float64(tok*r.SessionsPerMonth)
	}
	chars := roundTo(int(float64(tok)*3.7), 1000)
	pages := (chars + 1250) / 2500 // 500 words/page at ~5 chars a word
	measured := r.GeneratedAt.Format("2006-01-02")

	return []block{
		{"mine-headline", fmt.Sprintf(
			`On my own installation, measured %s: **%d items loaded, %d ever fired - %s utilization.**
That's ~%s dead tokens re-sent in every single session, ~%dk a month of
pure token waste, paid for on every request before you type anything.`,
			measured, loaded, fired, pct(fired, loaded), commas(tok), roundTo(monthly, 1000)/1000)},

		{"mine-costs", fmt.Sprintf(
			`- %s items loaded
- %s never used (%s)
- %s tok/session dead
- ~%s tok/month burned on irrelevant instructions
- ~$%.2f/month, ~$%.0f/year - the same waste priced instead of counted

<p align="center"><sub>The money line is one measurement of one stack, n=1, and the
weakest number here: <code>%s × %d × $%.2f ÷ 1e6</code> - input tokens only, at
<code>claude-sonnet-4-6</code>'s $%.2f/MTok default, with tokens estimated as
<code>ceil(chars / 3.7)</code> and the monthly session count extrapolated from a
%d-day window. Change the model, the price, or how much you work and it moves;
the item and token counts do not. See <a href="#limitations-transparency">Limitations</a>.</sub></p>

<p align="center"><em>Measured on my own setup - %d sessions over %d days, %s. Run <code>reap</code> to see yours.</em></p>`,
			spaced(loaded), spaced(dead), pctSpaced(dead, loaded), spaced(tok), spaced(roundTo(monthly, 1000)),
			r.MoneyPerMonth, r.MoneyPerMonth*12,
			spaced(tok), r.SessionsPerMonth, price, price,
			r.WindowDays, r.Sessions, r.WindowDays, measured)},

		{"mine-table", fmt.Sprintf(
			`| Before skillreaper | After skillreaper |
|---|---|
| %s items loaded every session | %d kept · %d actually fire |
| %s tok/session dead | Full context budget for real work |
| ≈ %s dead chars ≈ %d pages every session (at 500 words/pg) | Zero |
| Lower cache hit rate = higher latency | Smaller prompt fits in cache |

<sub>My own installation, measured %s.</sub>`,
			spaced(loaded), kept, fired, spaced(tok), spaced(chars), pages, measured)},
	}
}

// replace swaps the lines between one marker pair. Missing markers are an
// error, not a silent no-op: a renamed marker must fail loudly rather than
// leave a stale figure on the page.
func replace(doc, marker, body string) (string, error) {
	start := fmt.Sprintf("<!-- readme-%s:start -->", marker)
	end := fmt.Sprintf("<!-- readme-%s:end -->", marker)
	i := strings.Index(doc, start)
	j := strings.Index(doc, end)
	if i < 0 || j < 0 || j < i {
		return "", fmt.Errorf("README has no readme-%s marker pair - nothing to replace", marker)
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("refusing to write an empty readme-%s block", marker)
	}
	return doc[:i+len(start)] + "\n" + body + "\n" + doc[j:], nil
}

// pct mirrors the tool's own rounding so the README cannot disagree with what
// reap prints for the same run.
func pct(part, whole int) string {
	if whole == 0 {
		return "n/a"
	}
	p := part * 100 / whole
	if p == 0 && part > 0 {
		return "<1%"
	}
	return fmt.Sprintf("%d%%", p)
}

func pctSpaced(part, whole int) string { return strings.Replace(pct(part, whole), "%", " %", 1) }

// spaced and commas are the two thousands separators already in use on the
// page: "19 760" in the cost list, "19,760" in the sentence above it.
func spaced(n int) string { return group(n, " ") }
func commas(n int) string { return group(n, ",") }

func group(n int, sep string) string {
	s := fmt.Sprintf("%d", n)
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, out...), sep)
}

func roundTo(n, unit int) int { return (n + unit/2) / unit * unit }
