// Command reap scans a Claude Code installation, reports unused
// skills/MCP servers/agents with evidence from real transcripts, and
// prunes them reversibly.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// Version is set via -ldflags at release time.
var Version = "dev"

const usageText = `reap — evidence-based pruning for your Claude Code agent stack

Usage:
  reap [flags]              scan and report (read-only)
  reap prune [flags]        quarantine unused items (reversible)
  reap restore <id>|--all   undo prune actions
  reap version              print version

Flags:
`

type options struct {
	days        int
	minSessions int
	price       float64
	asJSON      bool
	asMarkdown  bool
	noColor     bool
	yes         bool
	all         bool
	claudeDir   string
	claudeJSON  string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("reap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts options
	fs.IntVar(&opts.days, "days", 30, "evidence window in days")
	fs.IntVar(&opts.minSessions, "min-sessions", 10, "sessions required before REAP verdicts")
	fs.Float64Var(&opts.price, "price", 3.0, "input price per million tokens (USD)")
	fs.BoolVar(&opts.asJSON, "json", false, "output JSON")
	fs.BoolVar(&opts.asMarkdown, "md", false, "output Markdown")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable colors")
	fs.BoolVar(&opts.yes, "yes", false, "prune: apply without confirmation")
	fs.BoolVar(&opts.all, "all", false, "restore: undo every prune action")
	fs.StringVar(&opts.claudeDir, "claude-dir", "", "Claude Code directory (default ~/.claude)")
	fs.StringVar(&opts.claudeJSON, "claude-json", "", "Claude config file (default ~/.claude.json)")
	fs.Usage = func() {
		fmt.Fprint(stderr, usageText)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if err := fillDefaults(&opts); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	switch cmd {
	case "":
		return cmdReport(opts, stdout, stderr)
	case "prune":
		return cmdPrune(opts, stdin, stdout, stderr)
	case "restore":
		return cmdRestore(opts, fs.Args(), stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "reap", Version)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		fs.Usage()
		return 2
	}
}

func fillDefaults(opts *options) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if opts.claudeDir == "" {
		opts.claudeDir = filepath.Join(home, ".claude")
	}
	if opts.claudeJSON == "" {
		opts.claudeJSON = filepath.Join(home, ".claude.json")
	}
	return nil
}

// gather runs every scanner plus the transcript parser and joins the
// result into a report.
func gather(opts options) (*report.Report, error) {
	if _, err := os.Stat(opts.claudeDir); err != nil {
		return nil, fmt.Errorf("no Claude Code installation found at %s", opts.claudeDir)
	}

	var items []scan.Item
	var warns []scan.Warning
	collect := func(i []scan.Item, w []scan.Warning) {
		items = append(items, i...)
		warns = append(warns, w...)
	}
	collect(scan.ScanSkills(opts.claudeDir))
	collect(scan.ScanAgents(opts.claudeDir))
	collect(scan.ScanMCP(opts.claudeJSON, opts.claudeDir))
	collect(scan.ScanHooks(opts.claudeDir))
	cwd, _ := os.Getwd()
	collect(scan.ScanProse(opts.claudeDir, cwd))

	cutoff := time.Now().AddDate(0, 0, -opts.days)
	st, err := usage.Parse(filepath.Join(opts.claudeDir, "projects"), cutoff, opts.days)
	if err != nil {
		return nil, err
	}

	return report.Build(items, st, warns, report.Opts{
		MinSessions:  opts.minSessions,
		PricePerMTok: opts.price,
		Cutoff:       cutoff,
	}), nil
}

func cmdReport(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	switch {
	case opts.asJSON:
		if err := report.RenderJSON(stdout, r); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	case opts.asMarkdown:
		report.RenderMarkdown(stdout, r)
	default:
		report.RenderText(stdout, r, colorEnabled(opts, stdout))
	}
	return 0
}

func colorEnabled(opts options, w io.Writer) bool {
	if opts.noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func cmdPrune(opts options, stdin io.Reader, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	var candidates []report.Row
	var skipped int
	for _, row := range r.Rows {
		if row.Verdict != report.VerdictReap {
			continue
		}
		if !row.Removable {
			skipped++
			continue
		}
		candidates = append(candidates, row)
	}

	if len(candidates) == 0 {
		fmt.Fprintln(stdout, "Nothing to reap. Your stack is clean (or evidence is insufficient).")
		if skipped > 0 {
			fmt.Fprintf(stdout, "%d unused plugin items can only be disabled via /plugin in Claude Code.\n", skipped)
		}
		return 0
	}

	fmt.Fprintf(stdout, "\n%d unused items can be reaped (quarantined, reversible):\n\n", len(candidates))
	for i, row := range candidates {
		fmt.Fprintf(stdout, "  [%d] %-8s %-40s %s\n", i+1, row.Category, row.Name, row.Source)
	}
	if skipped > 0 {
		fmt.Fprintf(stdout, "\n(%d unused plugin items skipped — disable those via /plugin)\n", skipped)
	}

	selected := candidates
	if !opts.yes {
		fmt.Fprint(stdout, "\nSelect items (e.g. 1,3,5), 'all', or empty to abort: ")
		sc := bufio.NewScanner(stdin)
		if !sc.Scan() {
			fmt.Fprintln(stdout, "aborted")
			return 0
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			fmt.Fprintln(stdout, "aborted")
			return 0
		}
		if line != "all" {
			selected = nil
			for _, part := range strings.Split(line, ",") {
				n, err := strconv.Atoi(strings.TrimSpace(part))
				if err != nil || n < 1 || n > len(candidates) {
					fmt.Fprintf(stderr, "invalid selection %q\n", part)
					return 2
				}
				selected = append(selected, candidates[n-1])
			}
		}
	}

	for _, row := range selected {
		var e prune.Entry
		var err error
		switch row.Category {
		case scan.CatMCP:
			scope := ""
			if strings.HasPrefix(row.Source, "project:") {
				scope = strings.TrimPrefix(row.Source, "project:")
			}
			e, err = prune.RemoveMCP(opts.claudeDir, row.Path, scope, row.Name)
		default:
			e, err = prune.QuarantineItem(opts.claudeDir, row.Item)
		}
		if err != nil {
			fmt.Fprintf(stderr, "error reaping %s: %v\n", row.Name, err)
			return 1
		}
		fmt.Fprintf(stdout, "reaped %s %s (id %s)\n", row.Category, row.Name, e.ID)
	}
	fmt.Fprintf(stdout, "\nDone. Undo anytime: reap restore --all (or a single id)\n")
	return 0
}

func cmdRestore(opts options, args []string, stdout, stderr io.Writer) int {
	if opts.all {
		n, err := prune.RestoreAll(opts.claudeDir)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "restored %d items\n", n)
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: reap restore <id>|--all")
		return 2
	}
	if err := prune.Restore(opts.claudeDir, args[0]); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "restored %s\n", args[0])
	return 0
}
