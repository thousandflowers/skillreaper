// Command reap scans a Claude Code installation, reports unused
// skills/MCP servers/agents with evidence from real transcripts, and
// prunes them reversibly.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/cost"
	"github.com/thousandflowers/skillreaper/internal/hook"
	"github.com/thousandflowers/skillreaper/internal/mute"
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
  reap by-project [flags]   skills bucketed by the project that fired them
  reap manifest <name>      emit a release manifest for one skill
  reap why <name>           explain in detail why an item got its verdict
  reap prune [flags]        quarantine unused items (reversible)
  reap keep <name>          mark item as keep (never prune)
  reap keep --list          show all kept items
  reap keep --remove <name>  remove item from keep list
  reap mute [<name>]        strip skill/agent descriptions (reversible)
  reap unmute <name>|--all  restore a muted skill's description
  reap restore <id>|--all   undo prune actions
  reap share [flags]        print a ready-to-share message about your savings
  reap install-hook         add a weekly SessionStart nudge to settings.json
  reap uninstall-hook       remove skillreaper's SessionStart nudge
  reap version              print version

Flags:
`

type options struct {
	days          int
	minSessions   int
	graceDays     int
	minTokens     int
	muteThreshold float64
	muteMinTokens int
	price         float64
	model         string
	asJSON        bool
	asMarkdown    bool
	noColor       bool
	yes           bool
	all           bool
	dryRun        bool
	quiet         bool
	noNudge       bool
	listKeep      bool
	removeKeep    string
	claudeDir     string
	claudeJSON    string
	claudeVersion string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts options
	fs.IntVar(&opts.days, "days", 30, "evidence window in days")
	fs.IntVar(&opts.minSessions, "min-sessions", 10, "sessions required before REAP verdicts")
	fs.IntVar(&opts.graceDays, "grace-days", 14, "items installed this recently → REVIEW(grace)")
	fs.IntVar(&opts.minTokens, "min-tokens", 3, "items below this token weight → KEEP(tiny)")
	fs.Float64Var(&opts.muteThreshold, "mute-threshold", 0.20, "MUTE used skills fired in fewer than this fraction of sessions (0 disables)")
	fs.IntVar(&opts.muteMinTokens, "mute-min-tokens", 50, "only MUTE skills heavier than this token weight")
	fs.StringVar(&opts.model, "model", "", "model ID for pricing lookup (overrides --price)")
	fs.Float64Var(&opts.price, "price", 0, "input price per million tokens (USD) — used when --model is unknown or unset")
	fs.BoolVar(&opts.asJSON, "json", false, "output JSON")
	fs.BoolVar(&opts.asMarkdown, "md", false, "output Markdown")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable colors")
	fs.BoolVar(&opts.noNudge, "no-nudge", false, "suppress the star-CTA prompt")
	fs.BoolVar(&opts.yes, "yes", false, "prune: apply without confirmation")
	fs.BoolVar(&opts.all, "all", false, "mute/restore/unmute: act on every eligible item")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "install-hook: print the change without writing")
	fs.BoolVar(&opts.quiet, "quiet", false, "suppress the normal text report")
	fs.BoolVar(&opts.listKeep, "list", false, "keep: list all kept items")
	fs.StringVar(&opts.removeKeep, "remove", "", "keep: remove a kept item")
	fs.StringVar(&opts.claudeDir, "claude-dir", "", "Claude Code directory (default ~/.claude)")
	fs.StringVar(&opts.claudeJSON, "claude-json", "", "Claude config file (default ~/.claude.json)")
	fs.StringVar(&opts.claudeVersion, "claude-version", "", "manifest: Claude Code version this skill was tested on")
	fs.Usage = func() {
		fmt.Fprint(stderr, usageText)
		fs.PrintDefaults()
	}
	// Go's flag.Parse stops at the first positional argument, so flags placed
	// after a subcommand or its name (e.g. `reap mute foo --claude-dir X`) would
	// be silently dropped and defaults used instead. parseInterspersed allows
	// flags anywhere; the leftover positionals are the subcommand and its args.
	positionals, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	cmd := ""
	var rest []string
	if len(positionals) > 0 {
		cmd, rest = positionals[0], positionals[1:]
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
	case "by-project":
		return cmdByProject(opts, stdout, stderr)
	case "manifest":
		return cmdManifest(opts, rest, stdout, stderr)
	case "why":
		return cmdWhy(opts, rest, stdout, stderr)
	case "keep":
		if opts.listKeep {
			return cmdKeepList(opts, stdout, stderr)
		}
		if opts.removeKeep != "" {
			return cmdKeepRemove(opts, opts.removeKeep, stdout, stderr)
		}
		return cmdKeep(opts, rest, stdout, stderr)
	case "mute":
		return cmdMute(opts, rest, stdin, stdout, stderr)
	case "unmute":
		return cmdUnmute(opts, rest, stdout, stderr)
	case "prune":
		return cmdPrune(opts, stdin, stdout, stderr)
	case "restore":
		return cmdRestore(opts, rest, stdout, stderr)
	case "install-hook":
		return cmdInstallHook(opts, stdout, stderr)
	case "uninstall-hook":
		return cmdUninstallHook(opts, stdout, stderr)
	case "share":
		return cmdShare(opts, stdout, stderr)
	case "nudge":
		return cmdNudge(opts, stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "reap", Version)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		fs.Usage()
		return 2
	}
}

// parseInterspersed parses flags that may appear before, between, or after
// positional arguments. flag.Parse stops at the first non-flag token, so we
// loop: parse, peel off one positional, parse the remainder, until no args are
// left. Returns the positional arguments in order.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, args[0])
		args = args[1:]
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

// requireClaudeDir guards commands that read or write skillreaper state under
// the Claude directory. When nothing was detected and none was given, claudeDir
// is empty and filepath.Join would resolve state paths relative to the current
// working directory. Fail clearly instead of silently polluting the cwd.
func requireClaudeDir(opts options, stderr io.Writer) bool {
	if strings.TrimSpace(opts.claudeDir) == "" {
		fmt.Fprintln(stderr, "error: no Claude Code directory found; pass --claude-dir <path>")
		return false
	}
	return true
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
	evidenceBlind := map[string]bool{}
	for _, p := range platforms {
		pid := string(p.ID)
		parsedAny := false
		mergeParsed := func(parsed *usage.Stats) {
			parsedAny = true
			if parsed.IncompleteEvidence && !evidenceBlind[pid] {
				evidenceBlind[pid] = true
				warns = append(warns, scan.Warning{
					Path: p.ConfigDirAbs,
					Msg:  fmt.Sprintf("%s usage evidence is incomplete because at least one transcript record exceeded the parser limit or could not be read; its items are shown as REVIEW, not REAP/MUTE.", p.Name),
				})
			}
			if st == nil {
				st = parsed
			} else {
				mergeStats(st, parsed)
			}
		}
		switch p.TranscriptType {
		case "jsonl":
			for _, td := range p.TranscriptDirs {
				parsed, err := usage.Parse(td, cutoff, opts.days)
				if err != nil {
					continue
				}
				mergeParsed(parsed)
			}
		case "sqlite":
			if p.TranscriptDB != "" {
				parsed, err := usage.ParseSQLite(p.TranscriptDB, cutoff, opts.days)
				if parsed != nil {
					mergeParsed(parsed)
				}
				switch {
				case err == nil:
				case errors.Is(err, usage.ErrNoSQLite):
					// CLI missing — handled by the evidence-blind block below.
				default:
					// A genuine parse failure: surface it but stay evidence-blind.
					warns = append(warns, scan.Warning{Path: p.TranscriptDB,
						Msg: fmt.Sprintf("%s SQLite evidence could not be read: %v", p.Name, err)})
				}
			}
		}
		// A platform that advertises transcripts but yielded no usable
		// evidence — OpenCode without the sqlite3 CLI, or no session files on
		// disk — is "evidence-blind". Its items must not be REAP'd or MUTE'd
		// on missing data, so flag the platform and tell the user why.
		if !parsedAny && p.HasTranscripts {
			evidenceBlind[pid] = true
			reason := "no session transcripts were found"
			if p.TranscriptType == "sqlite" {
				reason = "reading its SQLite history needs the sqlite3 CLI, which was not found in PATH"
			} else if p.TranscriptType != "jsonl" {
				reason = fmt.Sprintf("its transcripts use a format skillreaper does not parse yet (%s)", p.TranscriptType)
			}
			warns = append(warns, scan.Warning{
				Path: p.ConfigDirAbs,
				Msg:  fmt.Sprintf("%s usage is not counted because %s; its items are shown as REVIEW, not REAP/MUTE.", p.Name, reason),
			})
		}
	}
	if st == nil {
		st = usage.NewStats(opts.days)
	}

	keepSet, _ := override.KeepSet(opts.claudeDir)
	home, _ := os.UserHomeDir()
	claudeMD := scan.LoadClaudeMD(cwd, home)

	return report.Build(items, st, warns, report.Opts{
		MinSessions:   opts.minSessions,
		GraceDays:     opts.graceDays,
		MinTokens:     opts.minTokens,
		PricePerMTok:  opts.price,
		Cutoff:        cutoff,
		WindowDays:    opts.days,
		KeepSet:       keepSet,
		EvidenceBlind: evidenceBlind,
		ClaudeMDLines: claudeMD,
		MuteThreshold: opts.muteThreshold,
		MuteMinTokens: opts.muteMinTokens,
	}), nil
}

// mergeStats combines two usage stats into dst.
func mergeStats(dst, src *usage.Stats) {
	dst.Sessions += src.Sessions
	dst.FilesScanned += src.FilesScanned
	dst.MalformedLines += src.MalformedLines
	dst.IncompleteEvidence = dst.IncompleteEvidence || src.IncompleteEvidence
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
	for cat, errs := range src.Errors {
		for key, count := range errs {
			dst.Errors[cat][key] += count
		}
	}
	for cat, lasts := range src.LastAttempt {
		for key, ts := range lasts {
			if ts.After(dst.LastAttempt[cat][key]) {
				dst.LastAttempt[cat][key] = ts
			}
		}
	}
	for key, projs := range src.SkillProjects {
		if dst.SkillProjects[key] == nil {
			dst.SkillProjects[key] = map[string]int{}
		}
		for proj, count := range projs {
			dst.SkillProjects[key][proj] += count
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
	case opts.quiet:
		// audit silently — used to warm caches without printing
	default:
		col := colorEnabled(opts, stdout)
		report.RenderText(stdout, r, col)
		tryShowStarCta(opts, stdout, r, col)
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
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
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
	if !requireClaudeDir(opts, stderr) {
		return 1
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
		prompt := fmt.Sprintf("\nPrune all %d items? This quarantines them (reversible). [Y/n] ", len(candidates))
		if !confirm(stdin, stdout, prompt) {
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
	col := colorEnabled(opts, stdout)
	report.RenderValueFeedback(stdout, "pruned", len(candidates), totalTok, r.SessionsPerMonth, opts.price, col)
	tryShowShareHint(opts, stdout, col)
	tryShowStarCta(opts, stdout, r, col)
	return 0
}

func cmdRestore(opts options, args []string, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
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

// findItem locates a skill or agent row by its exact invocation key, then by
// the bare suffix of a namespaced key (so "plan" matches "ecc:plan"). Both
// categories are mutable, so `reap mute <name>` resolves either.
func findItem(r *report.Report, name string) (report.Row, bool) {
	mutable := func(row report.Row) bool {
		return row.Removable && (row.Category == scan.CatSkill || row.Category == scan.CatAgent)
	}
	for _, row := range r.Rows {
		if mutable(row) && row.Name == name {
			return row, true
		}
	}
	for _, row := range r.Rows {
		if !mutable(row) {
			continue
		}
		if i := strings.LastIndexByte(row.Name, ':'); i >= 0 && row.Name[i+1:] == name {
			return row, true
		}
	}
	return report.Row{}, false
}

// confirm prints prompt to stdout and reads a yes/no answer from stdin. A bare
// Enter (empty line) counts as yes; anything other than y/yes is a no.
func confirm(stdin io.Reader, stdout io.Writer, prompt string) bool {
	fmt.Fprint(stdout, prompt)
	sc := bufio.NewScanner(stdin)
	if !sc.Scan() {
		return false
	}
	line := strings.ToLower(strings.TrimSpace(sc.Text()))
	return line == "" || line == "y" || line == "yes"
}

func muteEligible(row report.Row) bool {
	return row.Path != "" &&
		row.Removable &&
		(row.Category == scan.CatSkill || row.Category == scan.CatAgent) &&
		(row.Verdict == report.VerdictReap || row.Verdict == report.VerdictMute)
}

func cmdMute(opts options, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	// Named single mute: reap mute <name>. Mutes the named skill or agent
	// regardless of verdict (the user asked for it explicitly).
	if len(args) > 0 {
		row, ok := findItem(r, args[0])
		if !ok {
			fmt.Fprintf(stderr, "no skill or agent found: %s\n", args[0])
			return 1
		}
		if err := mute.Mute(opts.claudeDir, row.Name, row.Path); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "muted %s — description stripped (~%d tok/session saved)\n", row.Name, row.Tokens)
		fmt.Fprintf(stdout, "Undo: reap unmute %s\n", row.Name)
		col := colorEnabled(opts, stdout)
		report.RenderValueFeedback(stdout, "muted", 1, row.Tokens, r.SessionsPerMonth, opts.price, col)
		tryShowShareHint(opts, stdout, col)
		return 0
	}

	// Bulk mute: reap mute (bare) or reap mute --all. Like prune, this rewrites
	// many files, so preview the candidates and confirm before acting.
	var candidates []report.Row
	for _, row := range r.Rows {
		if muteEligible(row) {
			candidates = append(candidates, row)
		}
	}
	if len(candidates) == 0 {
		fmt.Fprintln(stdout, "Nothing to mute.")
		return 0
	}
	fmt.Fprintf(stdout, "\n%d items eligible to mute (strips the injected description; reversible):\n", len(candidates))
	for _, row := range candidates {
		fmt.Fprintf(stdout, "  %-6s  %-40s  ~%d tok/session\n", row.Category, row.Name, row.Tokens)
	}
	if !opts.yes {
		if !confirm(stdin, stdout, fmt.Sprintf("\nMute all %d items? This strips their descriptions (reversible). [Y/n] ", len(candidates))) {
			fmt.Fprintln(stdout, "aborted")
			return 0
		}
	}
	muted, totalTok := 0, 0
	for _, row := range candidates {
		if err := mute.Mute(opts.claudeDir, row.Name, row.Path); err != nil {
			if errors.Is(err, mute.ErrAlreadyMuted) {
				continue
			}
			fmt.Fprintf(stderr, "error muting %s: %v\n", row.Name, err)
			return 1
		}
		fmt.Fprintf(stdout, "muted %s (~%d tok/session)\n", row.Name, row.Tokens)
		muted++
		totalTok += row.Tokens
	}
	fmt.Fprintf(stdout, "\nmuted %d items · ~%s tok/session reclaimed\n", muted, humanTok(totalTok))
	col := colorEnabled(opts, stdout)
	report.RenderValueFeedback(stdout, "muted", muted, totalTok, r.SessionsPerMonth, opts.price, col)
	tryShowShareHint(opts, stdout, col)
	return 0
}

func cmdUnmute(opts options, args []string, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	if opts.all {
		n, err := mute.UnmuteAll(opts.claudeDir)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "unmuted %d skills\n", n)
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: reap unmute <name>|--all")
		return 2
	}
	if err := mute.Unmute(opts.claudeDir, args[0]); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "unmuted %s\n", args[0])
	return 0
}

func cmdInstallHook(opts options, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	settings := filepath.Join(opts.claudeDir, "settings.json")
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "reap"
	}
	out, err := hook.Install(settings, hook.Command(exe), opts.dryRun)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if opts.dryRun {
		fmt.Fprintf(stdout, "dry-run — would write %s:\n%s\n", settings, out)
		return 0
	}
	fmt.Fprintf(stdout, "installed SessionStart nudge hook in %s\n", settings)
	fmt.Fprintf(stdout, "Undo: reap uninstall-hook\n")
	return 0
}

func cmdUninstallHook(opts options, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	settings := filepath.Join(opts.claudeDir, "settings.json")
	if err := hook.Uninstall(settings); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "removed skillreaper nudge hook from %s\n", settings)
	return 0
}

// cmdNudge is the SessionStart hook entry point: a passive, at-most-weekly
// reminder. It never fails loudly — a broken audit must not break a session.
func cmdNudge(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		return 0
	}
	st, err := hook.LoadNudgeState(opts.claudeDir)
	if err != nil {
		return 0
	}
	now := time.Now()
	if !hook.ShouldNudge(now, r.DeadCount, r.MuteCount, st) {
		return 0
	}
	fmt.Fprintf(stderr, "skillreaper: %d skills flagged for pruning since last check. Run `reap` to review.\n", r.DeadCount)
	st.LastNudgeAt = now
	st.LastReapCount = r.DeadCount
	st.LastMuteCount = r.MuteCount
	_ = hook.SaveNudgeState(opts.claudeDir, st)
	return 0
}

func cmdByProject(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if opts.asJSON {
		if err := report.RenderByProjectJSON(stdout, r); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	report.RenderByProject(stdout, r, colorEnabled(opts, stdout))
	return 0
}

func cmdManifest(opts options, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: reap manifest <name>")
		return 2
	}
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	m, ok := report.BuildManifest(r, args[0], opts.claudeDir, opts.claudeVersion)
	if !ok {
		fmt.Fprintf(stderr, "no skill found: %s\n", args[0])
		return 1
	}
	if opts.asJSON {
		if err := report.RenderManifestJSON(stdout, m); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	report.RenderManifestMarkdown(stdout, m)
	return 0
}

func cmdWhy(opts options, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: reap why <name>")
		return 2
	}
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	matches := report.MatchItems(r, args[0])
	if len(matches) == 0 {
		fmt.Fprintf(stderr, "no item found: %s\n", args[0])
		return 1
	}
	if len(matches) > 1 {
		fmt.Fprintf(stderr, "ambiguous: %q matches multiple items:\n", args[0])
		for _, m := range matches {
			fmt.Fprintf(stderr, "  %s\n", report.CanonicalName(m))
		}
		fmt.Fprintln(stderr, "qualify with a category, e.g. skill:<name>")
		return 1
	}
	row := matches[0]

	muted := false
	if names, e := mute.List(opts.claudeDir); e == nil {
		for _, n := range names {
			if n == row.Name {
				muted = true
				break
			}
		}
	}
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	claudeMD := scan.ClaudeMDReferences(scan.LoadClaudeMD(cwd, home), row.Name)

	e := report.BuildExplanation(row, r.Sessions, report.ExplainInput{
		MinSessions:   opts.minSessions,
		GraceDays:     opts.graceDays,
		MuteThreshold: opts.muteThreshold,
		WindowDays:    opts.days,
		Muted:         muted,
		ClaudeMDRef:   claudeMD,
		Now:           time.Now(),
	})

	if opts.asJSON {
		if err := report.RenderWhyJSON(stdout, e); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	report.RenderWhy(stdout, e, colorEnabled(opts, stdout))
	return 0
}

func cmdShare(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	total := r.DeadTokensPerSession + r.MuteTokensPerSession
	switch {
	case opts.asJSON:
		report.RenderShareJSON(stdout, total)
	case opts.asMarkdown:
		report.RenderShareMarkdown(stdout, total)
	default:
		report.RenderShareText(stdout, total)
	}
	return 0
}

// tryShowShareHint prints the share-command hint when conditions are met:
// not disabled, not json/md, TTY+color, and throttled to 30 days.
// It shares throttle state between prune and mute via NudgeState.
func tryShowShareHint(opts options, stdout io.Writer, color bool) {
	if isNudgeDisabled(opts) {
		return
	}
	if opts.asJSON || opts.asMarkdown {
		return
	}
	if !color {
		return
	}
	st, err := hook.LoadNudgeState(opts.claudeDir)
	if err != nil {
		return
	}
	now := time.Now()
	if !hook.ShouldShowShareHint(now, st) {
		return
	}
	report.RenderShareHint(stdout, true)
	st.LastShareHintAt = now
	st.ShareHintCount++
	_ = hook.SaveNudgeState(opts.claudeDir, st)
}

func humanTok(n int) string {
	switch {
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// isNudgeDisabled checks whether the user has opted out of the star-CTA
// via the --no-nudge flag or the SKILLREAPER_NO_NUDGE env var.
func isNudgeDisabled(opts options) bool {
	if opts.noNudge {
		return true
	}
	return os.Getenv("SKILLREAPER_NO_NUDGE") != ""
}

func tryShowStarCta(opts options, stdout io.Writer, r *report.Report, color bool) {
	if isNudgeDisabled(opts) {
		return
	}
	if opts.asJSON || opts.asMarkdown {
		return
	}
	if !color {
		return
	}
	if r.DeadTokensPerSession < report.MinStarCtaTokens {
		return
	}
	st, err := hook.LoadNudgeState(opts.claudeDir)
	if err != nil {
		return
	}
	now := time.Now()
	if !hook.ShouldShowStarCta(now, st) {
		return
	}
	report.RenderStarCta(stdout, r.DeadTokensPerSession, true)
	st.LastStarCtaAt = now
	st.StarCtaCount++
	_ = hook.SaveNudgeState(opts.claudeDir, st)
}
