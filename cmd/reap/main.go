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
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/cost"
	"github.com/thousandflowers/skillreaper/internal/override"
	"github.com/thousandflowers/skillreaper/internal/platform"
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
  reap gap [flags]          loaded-vs-fired utilization breakdown
  reap prune [flags]        quarantine unused items (reversible)
  reap keep <name>          mark item as keep (never prune)
  reap keep --list          show all kept items
  reap keep --remove <name>  remove item from keep list
  reap restore <id>|--all   undo prune actions
  reap version              print version

Flags:
`

type options struct {
	days        int
	minSessions int
	graceDays   int
	minTokens   int
	price       float64
	model       string
	asJSON      bool
	asMarkdown  bool
	noColor     bool
	yes         bool
	all         bool
	listKeep    bool
	removeKeep  string
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
	fs.IntVar(&opts.graceDays, "grace-days", 14, "items installed this recently → REVIEW(grace)")
	fs.IntVar(&opts.minTokens, "min-tokens", 3, "items below this token weight → KEEP(tiny)")
	fs.StringVar(&opts.model, "model", "", "model ID for pricing lookup (overrides --price)")
	fs.Float64Var(&opts.price, "price", 0, "input price per million tokens (USD) — used when --model is unknown or unset")
	fs.BoolVar(&opts.asJSON, "json", false, "output JSON")
	fs.BoolVar(&opts.asMarkdown, "md", false, "output Markdown")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable colors")
	fs.BoolVar(&opts.yes, "yes", false, "prune: apply without confirmation")
	fs.BoolVar(&opts.all, "all", false, "restore: undo every prune action")
	fs.BoolVar(&opts.listKeep, "list", false, "keep: list all kept items")
	fs.StringVar(&opts.removeKeep, "remove", "", "keep: remove a kept item")
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
	case "gap":
		return cmdGap(opts, stdout, stderr)
	case "keep":
		if opts.listKeep {
			return cmdKeepList(opts, stdout, stderr)
		}
		if opts.removeKeep != "" {
			return cmdKeepRemove(opts, opts.removeKeep, stdout, stderr)
		}
		return cmdKeep(opts, fs.Args(), stdout, stderr)
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
	if opts.claudeDir == "" {
		detected := platform.Detect()
		for _, p := range detected {
			if p.ID == platform.ClaudeCode {
				opts.claudeDir = p.ConfigDirAbs
				opts.claudeJSON = p.ConfigFileAbs
				break
			}
		}
	}
	// Price: --model lookup takes priority, then --price, then default.
	if opts.model != "" {
		if p, ok := cost.LookupPrice(opts.model); ok {
			opts.price = p
		}
	}
	if opts.price == 0 {
		if p, ok := cost.LookupPrice(cost.DefaultModel); ok {
			opts.price = p
		}
	}
	return nil
}

// gather runs every scanner plus transcript parsers across all
// detected platforms and joins the result into a report.
func gather(opts options) (*report.Report, error) {
	var platforms []platform.Info

	if opts.claudeDir != "" {
		// Override mode: --claude-dir was provided (test fixture or manual).
		p := platform.Info{
			ID:             platform.ClaudeCode,
			Name:           "Claude Code",
			ConfigDirAbs:   opts.claudeDir,
			ConfigFileAbs:  opts.claudeJSON,
			TranscriptType: "jsonl",
			TranscriptDirs: []string{filepath.Join(opts.claudeDir, "projects")},
		}
		if _, err := os.Stat(p.ConfigDirAbs); err != nil {
			return nil, fmt.Errorf("no Claude Code installation found at %s", opts.claudeDir)
		}
		platforms = append(platforms, p)
	} else {
		platforms = platform.Detect()
		if len(platforms) == 0 {
			return nil, fmt.Errorf("no supported AI coding platform found")
		}
	}

	var items []scan.Item
	var warns []scan.Warning
	cwd, _ := os.Getwd()
	collect := func(i []scan.Item, w []scan.Warning) {
		items = append(items, i...)
		warns = append(warns, w...)
	}

	for _, p := range platforms {
		dir := p.ConfigDirAbs
		pid := string(p.ID)

		collect(scan.ScanSkills(dir, pid))
		collect(scan.ScanAgents(dir, pid))
		collect(scan.ScanMCP(p.ConfigFileAbs, dir, pid))
		collect(scan.ScanHooks(dir, pid))
		collect(scan.ScanProse(dir, cwd, pid))

		for _, proseDir := range p.ProseDirs {
			if info, err := os.Stat(proseDir); err == nil && info.IsDir() {
				collect(scan.ScanProse(proseDir, "", pid))
			}
		}
	}

	cutoff := time.Now().AddDate(0, 0, -opts.days)

	var st *usage.Stats
	for _, p := range platforms {
		if p.TranscriptType != "jsonl" || len(p.TranscriptDirs) == 0 {
			continue
		}
		for _, td := range p.TranscriptDirs {
			parsed, err := usage.Parse(td, cutoff, opts.days)
			if err != nil {
				continue
			}
			if st == nil {
				st = parsed
			} else {
				mergeStats(st, parsed)
			}
		}
	}
	if st == nil {
		st = usage.NewStats(opts.days)
	}

	keepSet, _ := override.KeepSet(opts.claudeDir)

	return report.Build(items, st, warns, report.Opts{
		MinSessions:  opts.minSessions,
		GraceDays:    opts.graceDays,
		MinTokens:    opts.minTokens,
		PricePerMTok: opts.price,
		Cutoff:       cutoff,
		WindowDays:   opts.days,
		KeepSet:      keepSet,
	}), nil
}

// mergeStats combines two usage stats into dst.
func mergeStats(dst, src *usage.Stats) {
	dst.Sessions += src.Sessions
	dst.FilesScanned += src.FilesScanned
	dst.MalformedLines += src.MalformedLines
	for cat, uses := range src.Uses {
		for key, count := range uses {
			dst.Uses[cat][key] += count
		}
	}
	for cat, lasts := range src.Last {
		for key, ts := range lasts {
			if ts.After(dst.Last[cat][key]) {
				dst.Last[cat][key] = ts
			}
		}
	}
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

func cmdGap(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	switch {
	case opts.asJSON:
		if err := report.RenderGapJSON(stdout, r); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	case opts.asMarkdown:
		report.RenderGapMarkdown(stdout, r)
	default:
		report.RenderGap(stdout, r, colorEnabled(opts, stdout))
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

func cmdKeepRemove(opts options, name string, stdout, stderr io.Writer) int {
	if err := override.RemoveKeep(opts.claudeDir, name); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "removed keep: %s\n", name)
	return 0
}

func cmdKeepList(opts options, stdout, stderr io.Writer) int {
	items, err := override.ListKeep(opts.claudeDir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "No items kept. Mark items with: reap keep <name>")
		return 0
	}
	fmt.Fprintln(stdout, "Kept items (never pruned):")
	for _, item := range items {
		fmt.Fprintf(stdout, "  %s\n", item)
	}
	return 0
}

func cmdKeep(opts options, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: reap keep <name>")
		return 2
	}

	itemKey := strings.ToLower(args[0])
	if err := override.AddKeep(opts.claudeDir, itemKey); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "marked as keep: %s\n", itemKey)
	fmt.Fprintf(stdout, "This item will be excluded from prune. Undo: reap keep --remove %s\n", itemKey)
	return 0
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

	totalTok := 0
	for _, row := range candidates {
		totalTok += row.Tokens
	}
	fmt.Fprintf(stdout, "\n🧹  %d items unused · reclaim ~%s tok/session\n\n", len(candidates), humanTok(totalTok))
	for _, row := range candidates {
		weight := fmt.Sprintf("~%d tok", row.Tokens)
		if row.Category == scan.CatMCP || row.Category == scan.CatHook {
			weight = "?"
		}
		fmt.Fprintf(stdout, "  %-6s  %-40s  %s\n", row.Category, row.Name, weight)
	}
	if skipped > 0 {
		fmt.Fprintf(stdout, "\n  (%d unused plugin items skipped — disable via /plugin)\n", skipped)
	}

	if !opts.yes {
		fmt.Fprintf(stdout, "\nPrune all %d items? This quarantines them (reversible). [Y/n] ", len(candidates))
		sc := bufio.NewScanner(stdin)
		if !sc.Scan() {
			fmt.Fprintln(stdout, "aborted")
			return 0
		}
		line := strings.TrimSpace(sc.Text())
		if line != "" && strings.ToLower(line) != "y" && strings.ToLower(line) != "yes" {
			fmt.Fprintln(stdout, "aborted")
			return 0
		}
	}

	selected := candidates
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

func humanTok(n int) string {
	switch {
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
