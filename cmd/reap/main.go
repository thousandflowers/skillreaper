// Command reap scans a Claude Code installation, reports unused
// skills/MCP servers/agents with evidence from real transcripts, and
// prunes them reversibly.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/banner"
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

// version returns the build version. goreleaser injects it via -ldflags; for
// `go install ...@latest` it falls back to the module version recorded in the
// build info, so users see a real version instead of "dev".
func version() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return Version
}

// platformNames renders every supported platform as prose for the usage
// banner. Deriving it from platform.All means adding a platform cannot leave
// the banner claiming a shorter list, which is exactly how Gemini CLI went
// missing from it.
func platformNames() string {
	all := platform.All()
	names := make([]string, len(all))
	for i, p := range all {
		names[i] = p.Name
	}
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

const usageText = `reap — evidence-based pruning for your AI-agent stack
Reads %s.

Usage:
  reap [flags]              scan and report (read-only)
  reap gap [flags]          loaded-vs-fired utilization breakdown
  reap by-project [flags]   skills bucketed by the project that fired them
  reap route [flags]        propose a usage-informed lazy-load routing plan
  reap apm [flags]          emit a proposed APM apm.yml from per-repo firing
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
	days           int
	minSessions    int
	graceDays      int
	minTokens      int
	muteThreshold  float64
	muteMinTokens  int
	routeThreshold float64
	routeMinSkills int
	top            int
	apmDiff        string
	price          float64
	model          string
	asJSON         bool
	asMarkdown     bool
	asAgent        bool
	noColor        bool
	yes            bool
	all            bool
	dryRun         bool
	quiet          bool
	noNudge        bool
	noBanner       bool
	listKeep       bool
	removeKeep     string
	claudeDir      string
	claudeJSON     string
	claudeVersion  string

	// claudeDirExplicit records that --claude-dir was passed. claudeDir itself
	// cannot carry that meaning: fillDefaults also fills it from detection so
	// the state commands have a directory, and gather must not read that as a
	// request to scan Claude Code alone.
	claudeDirExplicit bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts options
	var showVersion bool
	fs.BoolVar(&showVersion, "version", false, "print version")
	fs.BoolVar(&showVersion, "v", false, "print version")
	fs.IntVar(&opts.days, "days", 30, "evidence window in days")
	fs.IntVar(&opts.minSessions, "min-sessions", 10, "sessions required before REAP verdicts")
	fs.IntVar(&opts.graceDays, "grace-days", 14, "items installed this recently → REVIEW(grace)")
	fs.IntVar(&opts.minTokens, "min-tokens", 3, "items below this token weight → KEEP(tiny)")
	fs.Float64Var(&opts.muteThreshold, "mute-threshold", 0.20, "MUTE used skills fired in fewer than this fraction of sessions (0 disables)")
	fs.IntVar(&opts.muteMinTokens, "mute-min-tokens", 50, "only MUTE skills heavier than this token weight")
	fs.Float64Var(&opts.routeThreshold, "route-threshold", 0.10, "route: skills fired in fewer than this fraction of sessions get routed behind a leaf router")
	fs.IntVar(&opts.routeMinSkills, "route-min-skills", 0, "route: skip the plan unless at least this many skills survive a prune (0 = always show)")
	fs.IntVar(&opts.top, "top", report.AgentMaxRows, "--agent: cap the dead-item table at this many rows")
	fs.StringVar(&opts.apmDiff, "diff", "", "apm: reconcile the proposed manifest against an existing apm.yml at this path")
	fs.StringVar(&opts.model, "model", "", "model ID for pricing and token-ratio lookup (overrides --price)")
	fs.Float64Var(&opts.price, "price", 0, "input price per million tokens (USD) — used when --model is unknown or unset")
	fs.BoolVar(&opts.asJSON, "json", false, "output JSON")
	fs.BoolVar(&opts.asMarkdown, "md", false, "output Markdown")
	fs.BoolVar(&opts.asAgent, "agent", false, "output compact plain text for an agent to paste verbatim")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable colors")
	fs.BoolVar(&opts.noNudge, "no-nudge", false, "suppress the star-CTA prompt")
	fs.BoolVar(&opts.noBanner, "no-banner", false, "suppress the wordmark shown above the default report and the usage text")
	fs.BoolVar(&opts.yes, "yes", false, "prune: apply without confirmation")
	fs.BoolVar(&opts.all, "all", false, "mute/restore/unmute: act on every eligible item")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "install-hook: print the change without writing")
	fs.BoolVar(&opts.quiet, "quiet", false, "suppress the normal text report")
	fs.BoolVar(&opts.listKeep, "list", false, "keep: list all kept items")
	fs.StringVar(&opts.removeKeep, "remove", "", "keep: remove a kept item")
	fs.StringVar(&opts.claudeDir, "claude-dir", "", "Claude Code directory (default ~/.claude)")
	fs.StringVar(&opts.claudeJSON, "claude-json", "", "Claude config file (default ~/.claude.json)")
	fs.StringVar(&opts.claudeVersion, "claude-version", "", "manifest: Claude Code version this skill was tested on")
	// flag calls Usage the moment it sees -h, before any flag written after it
	// has been parsed, so the opt-out has to be legible straight from args or
	// `reap --help --no-banner` would print the thing it just opted out of.
	optedOut := noBannerRequested(args)
	fs.Usage = func() {
		o := bannerOptions(opts)
		o.NoBanner = o.NoBanner || optedOut
		banner.Print(stderr, stdout, o)
		fmt.Fprintf(stderr, usageText, platformNames())
		fs.PrintDefaults()
	}
	// Go's flag.Parse stops at the first positional argument, so flags placed
	// after a subcommand or its name (e.g. `reap mute foo --claude-dir X`) would
	// be silently dropped and defaults used instead. parseInterspersed allows
	// flags anywhere; the leftover positionals are the subcommand and its args.
	positionals, err := parseInterspersed(fs, args)
	if err != nil {
		// -h/--help is not a usage error: flag already printed usage, exit 0.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if showVersion {
		fmt.Fprintln(stdout, "reap", version())
		return 0
	}
	// Rejected rather than clamped: an out-of-range value has no sensible
	// reading, and clamping would silently answer a different question than
	// the one asked.
	if msgs := outOfRangeFlags(opts); len(msgs) > 0 {
		for _, m := range msgs {
			fmt.Fprintf(stderr, "error: %s\n", m)
		}
		return 1
	}

	cmd := ""
	var rest []string
	if len(positionals) > 0 {
		cmd, rest = positionals[0], positionals[1:]
	}

	if err := fillDefaults(&opts, stderr); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	switch cmd {
	case "":
		// No banner here: a bare `reap` is the scan, and the wordmark belongs
		// to the surfaces a user looks at, never in front of working output.
		// The usage text is the only call site.
		return cmdReport(opts, stdout, stderr)
	case "gap":
		return cmdGap(opts, stdout, stderr)
	case "by-project":
		return cmdByProject(opts, stdout, stderr)
	case "route":
		return cmdRoute(opts, stdout, stderr)
	case "apm":
		return cmdApm(opts, stdout, stderr)
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
		fmt.Fprintln(stdout, "reap", version())
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", cmd)
		fs.Usage()
		return 2
	}
}

// noBannerRequested reports whether --no-banner appears anywhere in args,
// without waiting for the flag package to reach it.
func noBannerRequested(args []string) bool {
	for _, a := range args {
		name, value, hasValue := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if name != "no-banner" {
			continue
		}
		return !hasValue || (value != "false" && value != "0")
	}
	return false
}

// bannerOptions maps the output-format flags onto the banner's own view of
// them, so the suppression rules stay in one package instead of being restated
// at each call site.
func bannerOptions(o options) banner.Options {
	return banner.Options{
		JSON:     o.asJSON,
		Markdown: o.asMarkdown,
		Agent:    o.asAgent,
		NoBanner: o.noBanner,
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

// flagBound is one numeric flag's accepted range. Keeping the bounds in a table
// rather than a chain of ifs is what stops the next numeric flag from shipping
// unvalidated: --top used to be the only one checked, and every other flag was
// passed straight through to the report.
type flagBound struct {
	name     string
	val      float64
	min, max float64
	// why explains the bound when the number alone does not. Empty when
	// "must be at least N" already says everything.
	why string
}

// outOfRangeFlags returns one message per numeric flag whose value falls
// outside its accepted range, in flag order. An empty result means every flag
// is usable.
func outOfRangeFlags(opts options) []string {
	unbounded := math.Inf(1)
	bounds := []flagBound{
		// A window of zero or fewer days yields a complete, well-formed,
		// entirely empty report — "0 items never used" is indistinguishable
		// from a genuinely clean stack, which is the worst way to be wrong.
		{name: "--days", val: float64(opts.days), min: 1, max: unbounded, why: "a window of zero days produces an empty report that reads like a clean one"},
		{name: "--min-sessions", val: float64(opts.minSessions), min: 0, max: unbounded},
		{name: "--grace-days", val: float64(opts.graceDays), min: 0, max: unbounded},
		{name: "--min-tokens", val: float64(opts.minTokens), min: 0, max: unbounded},
		{name: "--mute-min-tokens", val: float64(opts.muteMinTokens), min: 0, max: unbounded},
		{name: "--route-min-skills", val: float64(opts.routeMinSkills), min: 0, max: unbounded},
		{name: "--top", val: float64(opts.top), min: 1, max: unbounded, why: "use --json for every row"},
		{name: "--price", val: opts.price, min: 0, max: unbounded, why: "a negative price renders as a negative monthly cost"},
		// Both thresholds are documented as a fraction of sessions. Above 1.0
		// the comparison is "fired in fewer than 100+% of sessions", which is
		// true of everything, so every used item changes verdict.
		{name: "--mute-threshold", val: opts.muteThreshold, min: 0, max: 1, why: "it is a fraction of sessions (0 disables)"},
		{name: "--route-threshold", val: opts.routeThreshold, min: 0, max: 1, why: "it is a fraction of sessions"},
	}

	var msgs []string
	for _, b := range bounds {
		if b.val >= b.min && b.val <= b.max {
			continue
		}
		var rng string
		if math.IsInf(b.max, 1) {
			rng = fmt.Sprintf("must be at least %s", trimFloat(b.min))
		} else {
			rng = fmt.Sprintf("must be between %s and %s", trimFloat(b.min), trimFloat(b.max))
		}
		msg := fmt.Sprintf("%s %s, got %s", b.name, rng, trimFloat(b.val))
		if b.why != "" {
			msg += " — " + b.why
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// trimFloat prints a bound without a trailing ".0", so an integer flag reads
// as "1" rather than "1.0" while a fractional one keeps its decimals.
func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func fillDefaults(opts *options, stderr io.Writer) error {
	opts.claudeDirExplicit = opts.claudeDir != ""
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
		} else {
			// An unknown --model used to fall through in silence, so a typo and
			// a model this build simply does not know produced identical,
			// default-priced reports. Say which model was asked for and which
			// price is standing in for it. Written to stderr rather than added
			// to Report.Warnings because it is a problem with the invocation,
			// not a caveat about the evidence — and stderr keeps --json clean.
			source := fmt.Sprintf("the default model (%s)", cost.DefaultModel)
			if opts.price > 0 {
				source = "--price"
			}
			fmt.Fprintf(stderr, "warning: unknown --model %q; pricing from %s\n", opts.model, source)
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

// dedupeByPath drops the same item found twice: ProseDirs names prose files
// that ScanProse already inventories, and listing one twice would double-count
// its tokens. Name is part of the key because several MCP servers legitimately
// share the config file that declares them — keying on the path alone would
// collapse them into one.
func dedupeByPath(items []scan.Item) []scan.Item {
	seen := make(map[string]bool, len(items))
	out := items[:0]
	for _, it := range items {
		key := string(it.Category) + "\x00" + it.Platform + "\x00" + it.Path + "\x00" + it.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}

// detectPlatforms is platform.Detect behind a variable so a test can supply a
// fixed platform set instead of whatever happens to be installed on the machine
// running the suite.
var detectPlatforms = platform.Detect

// gather runs every scanner plus transcript parsers across all
// detected platforms and joins the result into a report.
func gather(opts options) (*report.Report, error) {
	var platforms []platform.Info

	if opts.claudeDirExplicit {
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
		platforms = detectPlatforms()
		if len(platforms) == 0 {
			return nil, fmt.Errorf("no supported AI coding platform found")
		}
	}

	var items []scan.Item
	var warns []scan.Warning
	cwd, _ := os.Getwd()
	root := ""
	collect := func(i []scan.Item, w []scan.Warning) {
		for k := range i {
			i[k].RootDir = root
		}
		items = append(items, i...)
		warns = append(warns, w...)
	}

	for _, p := range platforms {
		dir := p.ConfigDirAbs
		pid := string(p.ID)
		root = dir

		collect(scan.ScanSkills(dir, pid))
		collect(scan.ScanAgents(dir, pid))
		collect(scan.ScanMCP(p.ConfigFileAbs, dir, pid))
		collect(scan.ScanHooks(dir, pid))
		collect(scan.ScanProse(dir, cwd, pid))

		for _, prosePath := range p.ProseDirs {
			info, err := os.Stat(prosePath)
			if err != nil {
				continue
			}
			if info.IsDir() {
				collect(scan.ScanProse(prosePath, "", pid))
			} else {
				collect(scan.ScanProseFile(prosePath, pid), nil)
			}
		}
	}

	items = dedupeByPath(items)

	cutoff := time.Now().AddDate(0, 0, -opts.days)

	var st *usage.Stats
	evidenceBlind := map[string]bool{}
	for _, p := range platforms {
		pid := string(p.ID)
		parsedAny := false
		var sqliteErr error
		mergeParsed := func(parsed *usage.Stats) {
			parsedAny = true
			if parsed.IncompleteEvidence && !evidenceBlind[pid] {
				evidenceBlind[pid] = true
				warns = append(warns, scan.Warning{
					Path:     p.ConfigDirAbs,
					Msg:      fmt.Sprintf("%s usage evidence is incomplete because at least one transcript record exceeded the parser limit or could not be read; its items are shown as REVIEW, not REAP/MUTE.", p.Name),
					Advisory: true,
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
				sqliteErr = err
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
		// A platform that yielded no usable evidence is "evidence-blind", and
		// there are two ways to get there: it advertises transcripts and none
		// could be read (OpenCode without the sqlite3 CLI, or no session files
		// on disk), or it exposes no transcripts at all, in which case no
		// amount of scanning will ever produce evidence about its items.
		// Either way the items must not be REAP'd or MUTE'd against a session
		// count that belongs to some other platform, so flag it and say why.
		if !parsedAny {
			evidenceBlind[pid] = true
			reason := "no session transcripts were found"
			switch {
			case !p.HasTranscripts:
				reason = "it exposes no session transcripts at all, so nothing can be observed about its items"
			case p.TranscriptType == "sqlite":
				if errors.Is(sqliteErr, usage.ErrNoSQLite) {
					reason = "reading its SQLite history needs the sqlite3 CLI, which was not found in PATH"
				} else if sqliteErr != nil {
					reason = fmt.Sprintf("its SQLite history could not be read: %v", sqliteErr)
				} else {
					reason = "no SQLite session history was found"
				}
			case p.TranscriptType != "jsonl":
				reason = fmt.Sprintf("its transcripts use a format skillreaper does not parse yet (%s)", p.TranscriptType)
			}
			warns = append(warns, scan.Warning{
				Path:     p.ConfigDirAbs,
				Msg:      fmt.Sprintf("%s usage is not counted because %s; its items are shown as REVIEW, not REAP/MUTE.", p.Name, reason),
				Advisory: true,
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
		Model:         opts.model,
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
	dst.UnreadableFiles += src.UnreadableFiles
	dst.TruncatedReads += src.TruncatedReads
	dst.OversizedLines += src.OversizedLines
	dst.UnparsableLines += src.UnparsableLines
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
	for key, p := range src.MCPPayload {
		d := dst.MCPPayload[key]
		d.Calls += p.Calls
		d.TotalChars += p.TotalChars
		d.NoiseChars += p.NoiseChars
		dst.MCPPayload[key] = d
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
	case opts.asAgent:
		// Its own case, like --json and --md: living outside the default branch
		// is what keeps RenderFooter and the star-CTA away from this format
		// without a hand-written gate. RenderAgent writes its own signature.
		report.RenderAgent(stdout, r, opts.top)
	case opts.quiet:
		// audit silently — used to warm caches without printing
	default:
		// The wordmark leads the report, which is what --no-banner has always
		// described ("shown above the default report and the usage text") and
		// what the README shows. Until now it only ever appeared on --help.
		// banner.Print applies its own gate: nothing for --json/--md/--agent,
		// nothing when the output is not a terminal, nothing when NO_COLOR is
		// set, so piped and machine-read output is unaffected.
		banner.Print(stderr, stdout, bannerOptions(opts))
		col := colorEnabled(opts, stdout)
		report.RenderText(stdout, r, col)
		tryShowStarCta(opts, stdout, r, col)
		// Permanent signature, deliberately outside every nudge gate above.
		// Living in this branch is what excludes it from --json/--md/--quiet.
		report.RenderFooter(stdout, col)
	}
	// Nothing inventoried while warnings were raised is a failed scan, and it
	// renders identically to a genuinely clean stack: the same banner, the same
	// "0 items never used", the same exit 0. The warnings say otherwise, but the
	// banner is the loudest thing on the page and a wrapping script sees only
	// the status. Name it and exit non-zero so the two can be told apart.
	// Only a warning that means "I could not read this" turns an empty
	// inventory into a failure. An empty directory raises advisory warnings
	// about missing evidence, reads perfectly, and is what a first run looks
	// like: exiting 1 there would make the very first command look broken.
	failed := 0
	for _, w := range r.Warnings {
		if !w.Advisory {
			failed++
		}
	}
	if len(r.Rows) == 0 && failed > 0 {
		noun := "warnings"
		if failed == 1 {
			noun = "warning"
		}
		fmt.Fprintf(stderr, "error: nothing could be inventoried, with %d read %s. A failed scan, not a clean stack.\n",
			failed, noun)
		return 1
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
	case opts.asAgent:
		report.RenderGapAgent(stdout, r)
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

	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	itemKey, err := resolveKeepKey(r, args[0])
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if err := override.AddKeep(opts.claudeDir, itemKey); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "marked as keep: %s\n", itemKey)
	fmt.Fprintf(stdout, "This item will be excluded from prune. Undo: reap keep --remove %s\n", itemKey)
	return 0
}

func cmdPrune(opts options, stdin io.Reader, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
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

	// --json and --quiet are non-interactive by definition: a confirmation
	// prompt cannot be answered on a stream someone is piping into jq, and it
	// cannot even be read on one they silenced. Both therefore describe the plan
	// and stop unless --yes was given as well.
	machine := opts.asJSON || opts.quiet

	totalTok := 0
	for _, row := range candidates {
		totalTok += row.Tokens
	}

	if len(candidates) == 0 {
		switch {
		case opts.asJSON:
			return writePruneJSON(stdout, stderr, nil, 0, skipped, false, nil)
		case opts.quiet:
			return 0
		}
		fmt.Fprintln(stdout, "Nothing to reap. Your stack is clean (or evidence is insufficient).")
		if skipped > 0 {
			fmt.Fprintf(stdout, "%d unused plugin items can only be disabled via /plugin in Claude Code.\n", skipped)
		}
		return 0
	}

	if !machine {
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
	}

	if !opts.yes {
		if machine {
			// Nothing was touched: report the plan and leave the tree alone.
			if opts.asJSON {
				return writePruneJSON(stdout, stderr, candidates, totalTok, skipped, false, nil)
			}
			return 0
		}
		prompt := fmt.Sprintf("\nPrune all %d items? This quarantines them (reversible). [Y/n] ", len(candidates))
		if !confirm(stdin, stdout, prompt) {
			fmt.Fprintln(stdout, "aborted")
			return 0
		}
	}

	selected := candidates
	// Serialise against other prunes on this directory. Quarantine is a
	// read-modify-write of the manifest, so two runs finishing at once leave
	// one set of moved files with no manifest entry — present in reaped/ and
	// invisible to restore. Measured before this lock existed: 501 skills in,
	// 148 restored after six concurrent runs.
	lock, lockErr := prune.AcquireLock(opts.claudeDir)
	if lockErr != nil {
		if errors.Is(lockErr, prune.ErrLocked) {
			fmt.Fprintln(stderr, "error: another prune is running against this directory; wait for it to finish")
			return 1
		}
		fmt.Fprintf(stderr, "error: could not take the prune lock: %v\n", lockErr)
		return 1
	}
	defer lock.Release()

	var done []prune.Entry
	// Items another process moved out from under this run. With the lock held
	// this should be empty; it stays handled because a file can still be
	// removed by hand between the report and the move.
	var alreadyGone []string
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
			// An item that vanished between the report and the move is not a
			// failure of this run: a concurrent prune, or a manual removal,
			// got there first. Aborting here left the earlier items
			// quarantined and the later ones untouched, on an error message
			// that named a missing file rather than the race that caused it.
			if errors.Is(err, prune.ErrAlreadyGone) {
				alreadyGone = append(alreadyGone, row.Name)
				continue
			}
			fmt.Fprintf(stderr, "error reaping %s: %v\n", row.Name, err)
			return 1
		}
		done = append(done, e)
		if !machine {
			fmt.Fprintf(stdout, "reaped %s %s (id %s)\n", row.Category, row.Name, e.ID)
		}
	}
	if opts.asJSON {
		return writePruneJSON(stdout, stderr, selected, totalTok, skipped, true, done)
	}
	if opts.quiet {
		return 0
	}
	if len(alreadyGone) > 0 {
		fmt.Fprintf(stderr, "skipped %d item(s) another process had already moved (e.g. %s); a second prune may be running\n",
			len(alreadyGone), alreadyGone[0])
	}
	fmt.Fprintf(stdout, "\nDone. Undo anytime: reap restore --all (or a single id)\n")
	col := colorEnabled(opts, stdout)
	// len(done), not len(candidates): with already-gone items skipped rather
	// than aborting the run, the two can differ, and the saving claimed has to
	// be the saving actually made.
	report.RenderValueFeedback(stdout, "pruned", len(done), totalTok, r.SessionsPerMonth, opts.price, col)
	tryShowShareHint(opts, stdout, col)
	tryShowStarCta(opts, stdout, r, col)
	return 0
}

// writePruneJSON emits the prune plan, or its result when --yes carried it out,
// as a single JSON document. Applied is explicit rather than implied by a
// non-empty entries list: a plan over zero candidates and a completed prune of
// zero candidates are different facts, and a caller checking whether its disk
// changed should not have to infer which one it got.
func writePruneJSON(w, stderr io.Writer, candidates []report.Row, totalTok, skipped int, applied bool, done []prune.Entry) int {
	type item struct {
		Category string `json:"category"`
		Name     string `json:"name"`
		Tokens   int    `json:"tokens"`
		Path     string `json:"path,omitempty"`
	}
	type entry struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Category string `json:"category"`
	}
	out := struct {
		Candidates         []item  `json:"candidates"`
		TotalTokens        int     `json:"total_tokens"`
		SkippedPluginItems int     `json:"skipped_plugin_items"`
		Applied            bool    `json:"applied"`
		Pruned             []entry `json:"pruned"`
	}{
		Candidates:         make([]item, 0, len(candidates)),
		TotalTokens:        totalTok,
		SkippedPluginItems: skipped,
		Applied:            applied,
		Pruned:             make([]entry, 0, len(done)),
	}
	for _, row := range candidates {
		out.Candidates = append(out.Candidates, item{
			Category: string(row.Category), Name: row.Name, Tokens: row.Tokens, Path: row.Path,
		})
	}
	for i, e := range done {
		name, cat := e.ID, ""
		if i < len(candidates) {
			name, cat = candidates[i].Name, string(candidates[i].Category)
		}
		out.Pruned = append(out.Pruned, entry{ID: e.ID, Name: name, Category: cat})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
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
	matches := make([]report.Row, 0, 2)
	for _, row := range r.Rows {
		if !mutable(row) {
			continue
		}
		if i := strings.LastIndexByte(row.Name, ':'); i >= 0 && row.Name[i+1:] == name {
			matches = append(matches, row)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
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

func cmdRoute(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	plan := report.BuildRoutePlan(r, opts.routeThreshold)
	if opts.routeMinSkills > 0 && plan.TotalSkills < opts.routeMinSkills {
		// Flag the skip on the plan so every output format renders it (parity).
		plan.Skipped = true
		plan.MinSkills = opts.routeMinSkills
	}
	switch {
	case opts.asJSON:
		if err := report.RenderRoutePlanJSON(stdout, plan); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	case opts.asMarkdown:
		report.RenderRoutePlanMarkdown(stdout, plan)
	default:
		report.RenderRoutePlan(stdout, plan, colorEnabled(opts, stdout))
	}
	return 0
}

func cmdApm(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	cwd, _ := os.Getwd()
	declared, lock, err := report.LoadAPMContext(opts.apmDiff, cwd)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	m := report.BuildAPM(r, cwd, lock, declared, opts.apmDiff)
	switch {
	case opts.asJSON:
		if err := report.RenderAPMJSON(stdout, m); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	case opts.asMarkdown:
		report.RenderAPMMarkdown(stdout, m)
	default:
		report.RenderAPMYAML(stdout, m)
	}
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
	// Every format that must stay clean is named here rather than relied on for
	// living outside the default branch of a switch. cmdPrune calls this too, and
	// a gate that is really just a position in one switch statement is one
	// refactor away from leaking a CTA into --quiet or into pasted --agent bytes.
	if opts.asJSON || opts.asMarkdown || opts.asAgent || opts.quiet {
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

// resolveKeepKey turns what the user typed into the category-qualified key that
// report.Build actually looks up.
//
// keep used to store args[0] verbatim, so only the exact "category:name" form
// ever matched. A bare name, a typo, or an item that does not exist was written
// to overrides.json and answered with "This item will be excluded from prune"
// while matching nothing. keep is the one command whose whole job is protecting
// an item from prune, so failing silently is the worst thing it can do.
func resolveKeepKey(r *report.Report, arg string) (string, error) {
	want := strings.ToLower(strings.TrimSpace(arg))
	if want == "" {
		return "", fmt.Errorf("empty item name")
	}
	seen := map[string]bool{}
	var matches []string
	add := func(key string) {
		if !seen[key] {
			seen[key] = true
			matches = append(matches, key)
		}
	}
	for _, row := range r.Rows {
		key := override.ItemKey(string(row.Category), row.Name)
		if key == want {
			// Already qualified: keep taking it, so existing scripts still work.
			return key, nil
		}
		name := strings.ToLower(row.Name)
		if name == want {
			add(key)
			continue
		}
		// Namespaced items ("ecc:plan") are commonly referred to by their leaf,
		// the same shorthand findItem accepts for why and mute.
		if i := strings.LastIndexByte(name, ':'); i >= 0 && name[i+1:] == want {
			add(key)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no item named %q in the inventory; run reap to list what is there", arg)
	default:
		return "", fmt.Errorf("%q matches %s; use the full key", arg, strings.Join(matches, ", "))
	}
}
