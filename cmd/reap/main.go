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
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/banner"
	"github.com/thousandflowers/skillreaper/internal/clock"
	"github.com/thousandflowers/skillreaper/internal/cost"
	"github.com/thousandflowers/skillreaper/internal/evidence"
	"github.com/thousandflowers/skillreaper/internal/hook"
	"github.com/thousandflowers/skillreaper/internal/mute"
	"github.com/thousandflowers/skillreaper/internal/override"
	"github.com/thousandflowers/skillreaper/internal/platform"
	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/reinvest"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/snapshot"
	"github.com/thousandflowers/skillreaper/internal/tty"
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
  reap menu                 choose what to prune, mute, lock or delete on one screen
                            (skillreaper with no arguments opens it too)
  reap gap [flags]          loaded-vs-fired utilization breakdown
  reap by-project [flags]   skills bucketed by the project that fired them
  reap route [flags]        propose a usage-informed lazy-load routing plan
  reap apm [flags]          emit a proposed APM apm.yml from per-repo firing
  reap manifest <name>      emit a release manifest for one skill
  reap why <name>           explain in detail why an item got its verdict
  reap prune [<name>]       quarantine one item, or every REAP item (reversible)
  reap keep <name>          mark item as keep (never prune)
  reap keep --list          show all kept items
  reap keep --remove <name>  remove item from keep list
  reap mute [<name>]        strip skill/agent descriptions (reversible)
  reap unmute [<name>|--all]  list muted skills, or restore a description
  reap restore [<id>|<name>|--all]  list pruned items, or undo a prune
  reap delete <name>        remove one item for good (cannot be restored)
  reap share [flags]        print a ready-to-share message about your savings
  reap reinvest [--dry-run] propose a skill for work none of yours covers, written from
                            your sessions, against mutes of the same weight (experimental,
                            asks Claude through claude -p)
  reap snapshot             save this run's --json payload for later comparison
  reap diff [<a> [<b>]]     compare two snapshots (default: newest vs previous)
  reap install-hook         add a weekly SessionStart nudge to settings.json
  reap uninstall-hook       remove skillreaper's SessionStart nudge
  reap version              print version
  reap help                 print this

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
	noCache        bool
	listKeep       bool
	removeKeep     string
	source         string
	claudeDir      string
	claudeJSON     string
	claudeVersion  string

	// claudeDirExplicit records that --claude-dir was passed. claudeDir itself
	// cannot carry that meaning: fillDefaults also fills it from detection so
	// the state commands have a directory, and gather must not read that as a
	// request to scan Claude Code alone.
	claudeDirExplicit bool
	// hasClaudeCode says opts.claudeDir is a Claude Code directory. On a machine
	// with only other tools, state lives under the first of those instead, and
	// what only Claude Code reads (settings.json, its retention) does not apply.
	hasClaudeCode bool
	// priceChosen says --price or a known --model set the price by hand.
	priceChosen bool
}

func main() {
	invoked := invokedName(os.Args[0])
	out := &stdoutTracker{f: os.Stdout, rewrite: hintRewriter(invoked, os.Args[1:])}
	errOut := &stdoutTracker{f: os.Stderr, rewrite: hintRewriter(invoked, nil)}
	args := os.Args[1:]
	if opensMenu(invoked, args) {
		args = []string{menuCommand}
	}
	code := run(args, os.Stdin, out, errOut)
	if code == 0 && out.err != nil {
		fmt.Fprintf(os.Stderr, "error: could not write the output: %v\n", out.err)
		code = 1
	}
	os.Exit(code)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// reinvest has flags of its own, so it reads its own command line.
	if len(args) > 0 && args[0] == reinvestCommand {
		return reinvest.Run(args[1:], stdin, stdout, stderr)
	}
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
	fs.StringVar(&opts.source, "source", "", "prune, mute: act only on items from this source (see the by-source summary)")
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
	fs.BoolVar(&opts.noCache, "no-cache", false, "re-parse the transcript corpus instead of reusing a cached parse")
	fs.BoolVar(&opts.yes, "yes", false, "prune, mute, delete, restore --all, unmute --all: apply without asking")
	fs.BoolVar(&opts.all, "all", false, "mute, restore, unmute: act on every eligible item")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "install-hook: print the change without writing")
	fs.BoolVar(&opts.quiet, "quiet", false, "suppress the normal text report")
	fs.BoolVar(&opts.listKeep, "list", false, "keep, snapshot: list what is saved")
	fs.StringVar(&opts.removeKeep, "remove", "", "keep: remove a kept item")
	fs.StringVar(&opts.claudeDir, "claude-dir", "", "Claude Code directory (default ~/.claude)")
	fs.StringVar(&opts.claudeJSON, "claude-json", "", "Claude config file (default ~/.claude.json)")
	fs.StringVar(&opts.claudeVersion, "claude-version", "", "manifest: Claude Code version this skill was tested on")
	// flag calls Usage the moment it sees -h, before any flag written after it
	// has been parsed, so the opt-out has to be legible straight from args or
	// `reap --help --no-banner` would print the thing it just opted out of.
	optedOut := noBannerRequested(args)
	printUsage := func(out io.Writer) {
		o := bannerOptions(opts)
		o.NoBanner = o.NoBanner || optedOut
		banner.Print(stderr, stdout, o)
		fs.SetOutput(out)
		fmt.Fprintf(out, usageText, platformNames())
		fs.PrintDefaults()
		fs.SetOutput(stderr)
	}
	// Help that was asked for is the output, so it goes where output goes and
	// can be read through a pager. Help shown for a mistake stays on stderr.
	fs.Usage = func() {
		if helpRequested(args) {
			printUsage(stdout)
			return
		}
		printUsage(stderr)
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
	if msg := invocationError(fs, cmd, rest); msg != "" {
		fmt.Fprintf(stderr, "error: %s\n", msg)
		return 2
	}
	if cmd == "help" {
		printUsage(stdout)
		return 0
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
	case menuCommand:
		return cmdMenu(opts, stdout, stderr)
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
		return cmdUnmute(opts, rest, stdin, stdout, stderr)
	case "prune":
		return cmdPrune(opts, rest, stdin, stdout, stderr)
	case "restore":
		return cmdRestore(opts, rest, stdin, stdout, stderr)
	case "delete":
		return cmdDelete(opts, rest, stdin, stdout, stderr)
	case "install-hook":
		return cmdInstallHook(opts, stdout, stderr)
	case "uninstall-hook":
		return cmdUninstallHook(opts, stdout, stderr)
	case "share":
		return cmdShare(opts, stdout, stderr)
	case "snapshot":
		return cmdSnapshot(opts, stdout, stderr)
	case "diff":
		return cmdDiff(opts, rest, stdout, stderr)
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

// helpRequested reports whether the arguments ask for help, as opposed to the
// flag package falling back on the usage text for a bad flag.
func helpRequested(args []string) bool {
	for _, a := range args {
		switch a {
		case "--":
			return false
		case "-h", "-help", "--help":
			return true
		}
	}
	return false
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
		consumed := len(args) - fs.NArg()
		terminated := consumed > 0 && args[consumed-1] == "--"
		args = fs.Args()
		// After a literal -- everything is a positional, however it looks.
		if terminated {
			return append(positionals, args...), nil
		}
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
		// entirely empty report — "0 items never fired" is indistinguishable
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
	opts.hasClaudeCode = opts.claudeDirExplicit
	if opts.claudeDir == "" {
		detected := platform.Detect()
		for _, p := range detected {
			if p.ID == platform.ClaudeCode {
				opts.claudeDir = p.ConfigDirAbs
				opts.claudeJSON = p.ConfigFileAbs
				opts.hasClaudeCode = true
				break
			}
		}
		// Without Claude Code the report still lists the other tools' items, so
		// the actions it offers need somewhere to keep their undo state.
		if opts.claudeDir == "" && len(detected) > 0 {
			opts.claudeDir = detected[0].ConfigDirAbs
		}
	}
	opts.priceChosen = opts.price > 0
	// Price: --model lookup takes priority, then --price, then default.
	if opts.model != "" {
		if p, ok := cost.LookupPrice(opts.model); ok {
			opts.price = p
			opts.priceChosen = true
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
//
// Prose is one file however many tools read it, so its key leaves the platform
// out: the first platform to list it holds its weight, and the same CLAUDE.md is
// not added to the total once per tool.
func dedupeByPath(items []scan.Item) []scan.Item {
	seen := make(map[string]bool, len(items))
	out := items[:0]
	for _, it := range items {
		platformKey := it.Platform
		if it.Category == scan.CatProse {
			platformKey = ""
		}
		key := string(it.Category) + "\x00" + platformKey + "\x00" + it.Path + "\x00" + it.Name
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
			HasSkills:      true,
			HasAgents:      true,
			HasMCP:         true,
			HasHooks:       true,
			HasProse:       true,
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

		// The platform table says what each tool has; a folder named the same way
		// under a tool that does not read it is not part of its stack.
		if p.HasSkills {
			collect(scan.ScanSkillsIn(dir, pid, p.NestedSkills))
			for _, extra := range p.SkillDirs {
				if extra != filepath.Join(dir, "skills") {
					collect(scan.ScanDeclaredSkills(extra, pid))
				}
			}
		}
		if p.HasAgents {
			collect(scan.ScanAgents(dir, pid))
		}
		if p.HasMCP {
			collect(scan.ScanMCP(p.ConfigFileAbs, dir, pid, p.ConfigFormat))
		}
		if p.HasHooks {
			collect(scan.ScanHooks(dir, pid))
		}
		projectDir := ""
		if p.ReadsProjectCLAUDE {
			projectDir = cwd
		}
		if p.HasProse {
			collect(scan.ScanProse(dir, projectDir, pid))
		}

		for _, prosePath := range p.ProseDirs {
			info, err := os.Stat(prosePath)
			if err != nil {
				continue
			}
			if info.IsDir() {
				// ScanProse takes the directory holding rules/, not rules/ itself.
				collect(scan.ScanProse(filepath.Dir(prosePath), "", pid))
			} else {
				collect(scan.ScanProseFile(prosePath, pid), nil)
			}
		}
	}

	items = dedupeByPath(items)

	cutoff := clock.Now().AddDate(0, 0, -opts.days)

	var st *usage.Stats
	// The durable evidence record, loaded before any parsing so transcripts
	// already counted are not counted twice.
	digestPath := evidence.Path(opts.claudeDir)
	digest := evidence.Load(digestPath)
	digestDirty := false

	evidenceBlind := map[string]bool{}
	blindReason := map[string]string{}
	perPlatform := map[string]*usage.Stats{}
	// cleanupPeriodDays governs Claude Code's transcripts and nothing else, so
	// the corpus it is compared against has to be Claude Code's alone. The
	// merged Stats spans every platform scanned, and other agents keep their
	// own history under their own rules — comparing that against this setting
	// reports a horizon that does not apply to it.
	claudeCorpus := usage.NewStats(opts.days)
	for _, p := range platforms {
		pid := string(p.ID)
		parsedAny := false
		var sqliteErr error
		mergeParsed := func(parsed *usage.Stats) {
			// A walk that found no session of this platform is no evidence, even
			// if it returned cleanly: its items must not be judged on the sessions
			// of some other platform.
			parsedAny = parsedAny || parsed.Sessions > 0
			if perPlatform[pid] == nil {
				perPlatform[pid] = usage.NewStats(opts.days)
			}
			mergeStats(perPlatform[pid], parsed)
			perPlatform[pid].ObserveCorpus(parsed.OldestTranscript, parsed.NewestTranscript)
			if p.ID == platform.ClaudeCode {
				claudeCorpus.ObserveCorpus(parsed.OldestTranscript, parsed.NewestTranscript)
			}
			if parsed.IncompleteEvidence && !evidenceBlind[pid] {
				evidenceBlind[pid] = true
				blindReason[pid] = "at least one transcript record exceeded the parser limit or could not be read"
				warns = append(warns, scan.Warning{
					Path:     p.ConfigDirAbs,
					Msg:      fmt.Sprintf("%s usage evidence is incomplete because at least one transcript record exceeded the parser limit or could not be read; its items are shown as REVIEW, not REAP/MUTE.", p.Name),
					Advisory: true,
				})
			}
			if parsed.PositiveOnly && parsed.Sessions > 0 && !evidenceBlind[pid] {
				evidenceBlind[pid] = true
				blindReason[pid] = "its history shows what fired but not everything that was used, so no fire proves nothing"
				warns = append(warns, scan.Warning{
					Path:     p.ConfigDirAbs,
					Msg:      fmt.Sprintf("%s: %s; its used items show as used and the rest are shown as REVIEW, not REAP/MUTE.", p.Name, blindReason[pid]),
					Advisory: true,
				})
			}
			if st == nil {
				st = usage.NewStats(opts.days)
			}
			mergeStats(st, parsed)
		}
		switch p.TranscriptType {
		case "codex-jsonl":
			// Sources get a prefix because an earlier version recorded these files
			// with no fires in them, and that record must not stop them being read.
			scoped := func(id string) string { return pid + ":" + id }
			for _, td := range p.TranscriptDirs {
				needs := func(id string) bool { return !digest.HasSource(scoped(id)) }
				observe := func(id string, one *usage.Stats) {
					digest.Add(scoped(id), scopedObservation(pid, observationFrom(one)))
					digestDirty = true
				}
				if parsed, err := usage.ParseCodex(td, cutoff, opts.days, needs, observe); err == nil {
					mergeParsed(parsed)
				}
			}
		case "jsonl":
			// Parsing the corpus is what costs: on an 800-transcript, 345 MB
			// tree, walking and stat-ing takes 22 ms and reading every byte
			// 0.33 s, while a full run takes about five seconds. Every
			// subcommand paid that in full, so `reap why <one item>` re-read
			// the whole corpus to answer a question about a single row.
			//
			// The fingerprint is built from stat alone and covers the file
			// set, sizes, modification times and the window. Transcripts are
			// append-only and a new session is a new file, so any change to
			// the evidence changes it. Anything unexpected is a miss.
			fp := ""
			if !opts.noCache {
				fp = usage.Fingerprint(p.TranscriptDirs, opts.days)
				if cached, ok := usage.LoadCache(opts.claudeDir, fp); ok {
					mergeParsed(cached)
					break
				}
			}
			var forCache *usage.Stats
			for _, td := range p.TranscriptDirs {
				// Fold each transcript into the durable digest as it is read.
				// Transcripts older than the platform's retention period are
				// deleted, so this is the only place evidence about anything
				// used less often than that can ever accumulate.
				needs := func(id string) bool { return !digest.HasSource(id) }
				observe := func(id string, one *usage.Stats) {
					digest.Add(id, scopedObservation(pid, observationFrom(one)))
					digestDirty = true
				}
				parsed, err := usage.ParseObserving(td, cutoff, opts.days, needs, observe)
				if err != nil {
					// A directory that failed to parse means the cache would
					// describe less than the corpus does. Do not store it.
					fp = ""
					continue
				}
				if forCache == nil {
					forCache = parsed
				} else {
					mergeStats(forCache, parsed)
				}
				mergeParsed(parsed)
			}
			if fp != "" && forCache != nil && p.ID == platform.ClaudeCode {
				usage.SaveCache(opts.claudeDir, fp, forCache)
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
			reason := fmt.Sprintf("no session transcripts were found in the last %d days", opts.days)
			switch {
			case !p.HasTranscripts:
				reason = "it exposes no session transcripts at all, so nothing can be observed about its items"
			case p.TranscriptType == "sqlite":
				if errors.Is(sqliteErr, usage.ErrNoSQLite) {
					reason = "reading its SQLite history needs the sqlite3 CLI, which was not found in PATH"
				} else if sqliteErr != nil {
					reason = fmt.Sprintf("its SQLite history could not be read: %v", sqliteErr)
				} else {
					reason = fmt.Sprintf("no session history was found (looked in %s)", p.TranscriptDBTried)
				}
			case p.TranscriptType != "jsonl" && p.TranscriptType != "codex-jsonl":
				reason = fmt.Sprintf("its transcripts use a format skillreaper does not parse yet (%s)", p.TranscriptType)
			}
			evidenceBlind[pid] = true
			blindReason[pid] = reason
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
	if w, ok := retentionWarning(opts, claudeCorpus); ok {
		warns = append(warns, w)
	}
	if digestDirty {
		// A failure to persist is not worth interrupting a report for: the
		// run is still correct, it simply did not extend the record.
		_ = evidence.Save(digestPath, digest)
	}

	keepSet, keepErr := override.KeepSet(opts.claudeDir)
	if keepErr != nil {
		warns = append(warns, scan.Warning{Path: override.Path(opts.claudeDir),
			Msg: fmt.Sprintf("the keep list cannot be read, so nothing is protected by it in this report: %v", keepErr)})
	}
	home, _ := os.UserHomeDir()
	claudeMD := scan.LoadClaudeMD(cwd, home, opts.claudeDir)

	names := map[string]string{}
	for _, p := range platform.All() {
		names[string(p.ID)] = p.Name
	}
	muted, muteLost := mutedSet(opts.claudeDir, items)
	price, priceBasis := pricing(opts, st)
	return report.Build(items, st, warns, report.Opts{
		PlatformNames: names,
		PriceBasis:    priceBasis,
		PrunedCopies:  prunedCopies(opts.claudeDir),
		// The durable record, consulted so an item that fired before the
		// retention sweep is not mistaken for one that never fired at all.
		History: func(platformID, category, name string) (int, time.Time, bool) {
			r, ok := digest.Lookup(category, scopedName(platformID, name))
			if !ok || r.Uses == 0 {
				return 0, time.Time{}, false
			}
			return r.Uses, r.Last, true
		},

		MinSessions:   opts.minSessions,
		GraceDays:     opts.graceDays,
		MinTokens:     opts.minTokens,
		Model:         opts.model,
		PricePerMTok:  price,
		Cutoff:        cutoff,
		WindowDays:    opts.days,
		KeepSet:       keepSet,
		EvidenceBlind: evidenceBlind,
		BlindReason:   blindReason,
		PerPlatform:   perPlatform,
		RetentionDays: retentionFor(opts.claudeDir),
		ClaudeMDLines: claudeMD,
		MuteThreshold: opts.muteThreshold,
		MuteMinTokens: opts.muteMinTokens,
		Muted:         muted,
		MuteLost:      muteLost,
		PruneLost:     prunedPluginItems(opts.claudeDir),
	}), nil
}

// mutedSet sorts the items reap has muted by what is true of the file now: still
// without a description is muted, with one again is a mute that was lost. Muting
// is per file, so the same name elsewhere is neither. State records resolved
// paths, so each item's path is resolved to compare. Unreadable state sets nothing
// apart: the skills are then judged like any other, and reap unmute reports it.
func mutedSet(claudeDir string, items []scan.Item) (muted, lost map[string]bool) {
	entries, err := mute.Entries(claudeDir)
	if err != nil || len(entries) == 0 {
		return nil, nil
	}
	recorded := make(map[string]bool, len(entries))
	for _, e := range entries {
		recorded[e.Path] = true
	}
	muted, lost = map[string]bool{}, map[string]bool{}
	for _, it := range items {
		if it.Path == "" {
			continue
		}
		p, err := filepath.EvalSymlinks(it.Path)
		if err != nil || !recorded[p] {
			continue
		}
		if mute.Strippable(p) {
			lost[it.Path] = true
		} else {
			muted[it.Path] = true
		}
	}
	return muted, lost
}

// pricing is the input price behind the money figures, and what it rests on. A
// price asked for wins; next comes the mix of models the transcripts show, and
// only without either a stated assumption.
func pricing(opts options, st *usage.Stats) (float64, string) {
	if opts.priceChosen {
		return opts.price, ""
	}
	if blended, ok := cost.Blended(st.ModelUse); ok {
		return blended, "the models in your transcripts"
	}
	return opts.price, "assumed, " + cost.DefaultModel
}

// prunedCopies maps where each pruned item came from to its manifest id, so a
// live item with a copy in quarantine can say so.
func prunedCopies(claudeDir string) map[string]string {
	entries, err := prune.LoadManifest(claudeDir)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, e := range entries {
		if !e.Restored && !prune.NeverMoved(e) && e.From != "" {
			out[e.From] = e.ID
		}
	}
	return out
}

// prunedPluginItems is every plugin item in quarantine, by the key report.Build
// looks for: any row of that key is one an update brought back.
func prunedPluginItems(claudeDir string) map[string]bool {
	entries, err := prune.LoadManifest(claudeDir)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, e := range entries {
		if !e.Restored && !prune.NeverMoved(e) && report.IsPluginSource(e.Source) {
			out[report.PruneLostKey(e.Platform, e.Category, e.Source, e.Name)] = true
		}
	}
	return out
}

// retentionFor is Claude Code's transcript retention for a directory.
func retentionFor(claudeDir string) int {
	days, _ := scan.RetentionDays(claudeDir)
	return days
}

// scopedName is an item's key in the durable digest. Each platform has its own
// record, as a use in one tool says nothing about the copy in another. Claude
// Code's keys stay bare, so digests already on disk keep their meaning.
func scopedName(platformID, name string) string {
	if platformID == "" || platformID == string(platform.ClaudeCode) {
		return name
	}
	return platformID + "|" + name
}

// scopedObservation returns o with its names under the platform's keys.
func scopedObservation(platformID string, o evidence.Observation) evidence.Observation {
	scope := func(in map[string]map[string]int) map[string]map[string]int {
		out := make(map[string]map[string]int, len(in))
		for cat, byName := range in {
			out[cat] = make(map[string]int, len(byName))
			for name, n := range byName {
				out[cat][scopedName(platformID, name)] = n
			}
		}
		return out
	}
	last := make(map[string]map[string]time.Time, len(o.Last))
	for cat, byName := range o.Last {
		last[cat] = make(map[string]time.Time, len(byName))
		for name, t := range byName {
			last[cat][scopedName(platformID, name)] = t
		}
	}
	return evidence.Observation{Uses: scope(o.Uses), Errors: scope(o.Errors), At: o.At, Last: last}
}

// mergeStats combines two usage stats into dst.
// corpusReachesHorizon is how close the corpus span must come to the retention
// window before the sweep is treated as actively trimming it. Below this, a
// short corpus means the user started recently, and telling them evidence is
// being destroyed would be a false alarm — the fastest way to teach someone to
// ignore every warning the tool prints.
const corpusReachesHorizon = 0.9

// retentionWarning describes the transcript retention horizon when it changes
// what a verdict can mean. Claude Code deletes transcripts older than
// cleanupPeriodDays, so that setting — not --days — is the real ceiling on how
// far back "never used" can reach, and nothing in the report said so.
//
// Two situations are worth saying out loud, and a third deliberately is not:
// asking for a window longer than the retention (the window cannot be
// satisfied), and a corpus that already reaches the horizon (the sweep is
// deleting evidence now). A corpus far shorter than the horizon is just a
// young installation and stays silent.
func retentionWarning(opts options, st *usage.Stats) (scan.Warning, bool) {
	if strings.TrimSpace(opts.claudeDir) == "" || st == nil || !opts.hasClaudeCode {
		return scan.Warning{}, false
	}
	retention, explicit := scan.RetentionDays(opts.claudeDir)
	setting := fmt.Sprintf("cleanupPeriodDays is not set, so Claude Code's default of %d days applies", retention)
	if explicit {
		setting = fmt.Sprintf("cleanupPeriodDays is set to %d", retention)
	}

	switch {
	case opts.days > retention:
		return scan.Warning{
			Path: opts.claudeDir,
			Msg: fmt.Sprintf(
				"the requested %d-day window cannot be filled: %s, and transcripts older than that are deleted, so %d days is the furthest back any verdict here can reach.",
				opts.days, setting, retention),
			Advisory: true,
		}, true

	case st.CorpusSpanDays() >= int(float64(retention)*corpusReachesHorizon):
		return scan.Warning{
			Path: opts.claudeDir,
			Msg: fmt.Sprintf(
				"the oldest transcript on disk is %d days old and %s, so the retention sweep is deleting evidence about as fast as it accumulates. Anything used less often than that is invisible here, not merely cold. Raise cleanupPeriodDays in settings.json to keep more history.",
				st.CorpusSpanDays(), setting),
			Advisory: true,
		}, true
	}
	return scan.Warning{}, false
}

// observationFrom converts one transcript's parsed stats into the shape the
// durable digest stores: counts by category and name, plus the latest
// timestamp seen in that transcript.
func observationFrom(one *usage.Stats) evidence.Observation {
	o := evidence.Observation{
		Uses:   map[string]map[string]int{},
		Errors: map[string]map[string]int{},
		Last:   map[string]map[string]time.Time{},
	}
	addUse := func(cat, name string, n int, ts time.Time) {
		if n == 0 {
			return
		}
		if o.Uses[cat] == nil {
			o.Uses[cat], o.Last[cat] = map[string]int{}, map[string]time.Time{}
		}
		o.Uses[cat][name] += n
		if ts.After(o.Last[cat][name]) {
			o.Last[cat][name] = ts
		}
		if ts.After(o.At) {
			o.At = ts
		}
	}
	for cat, byName := range one.Uses {
		for name, n := range byName {
			addUse(string(cat), name, n, one.Last[cat][name])
		}
	}
	// Uses before the window are not use in the window, and are history all the
	// same: this record is what the older verdicts read.
	olderUses, olderLast := one.OlderUses()
	for cat, byName := range olderUses {
		for name, n := range byName {
			addUse(string(cat), name, n, olderLast[cat][name])
		}
	}
	for cat, byName := range one.Errors {
		for name, n := range byName {
			if n == 0 {
				continue
			}
			if o.Errors[string(cat)] == nil {
				o.Errors[string(cat)] = map[string]int{}
			}
			o.Errors[string(cat)][name] += n
		}
	}
	return o
}

func mergeStats(dst, src *usage.Stats) {
	dst.Sessions += src.Sessions
	dst.FilesScanned += src.FilesScanned
	dst.SkippedByCutoff += src.SkippedByCutoff
	// Counts, evidence flags and measured totals merge in usage.MergeInto: they
	// are sums over messages, and a message belongs to exactly one transcript.
	// The corpus bracket is deliberately NOT merged. It is only ever compared
	// against a per-platform setting (cleanupPeriodDays is Claude Code's), and a
	// merged span reads like one platform's while describing all of them — which
	// is the bug this warning shipped with in its first draft: an 81-day corpus
	// belonging to another agent, measured against Claude Code's 30-day horizon.
	// Callers needing a span must take it from the per-platform Stats.
	usage.MergeInto(dst, src)
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
		report.RenderReinvestHint(stdout, report.ReinvestPath(), col)
		// Permanent signature, deliberately outside every nudge gate above.
		// Living in this branch is what excludes it from --json/--md/--quiet.
		report.RenderFooter(stdout, col)
	}
	// Nothing inventoried while warnings were raised is a failed scan, and it
	// renders identically to a genuinely clean stack: the same banner, the same
	// "0 items never fired", the same exit 0. The warnings say otherwise, but the
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
	if opts.noColor || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return tty.IsTerminal(w)
}

func cmdKeepRemove(opts options, name string, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	removed, err := override.RemoveKeep(opts.claudeDir, name)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "removed keep: %s\n", removed)
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
	// The key is one name, so it holds every copy of that name on every platform.
	for _, row := range r.Rows {
		if override.ItemKey(string(row.Category), row.Name) == itemKey {
			fmt.Fprintf(stdout, "  covers %s: %s\n", row.Platform, row.Path)
		}
	}
	fmt.Fprintf(stdout, "This item will be excluded from prune. Undo: reap keep --remove %s\n", itemKey)
	return 0
}

// findItem locates the row a named prune or delete acts on. See findRow for how a
// name is read. A name that reaches a removable row means that row, as it always
// did; only a name that reaches nothing else goes on to a plugin's item, a link or
// a shared folder. A server of a config reap cannot edit is not offered at all.
func findItem(r *report.Report, name string) ([]report.Row, error) {
	return findPreferring(r, name, func(row report.Row) bool {
		return row.Category == scan.CatSkill || row.Category == scan.CatAgent ||
			row.Category == scan.CatMCP && (row.Removable || report.IsPluginSource(row.Source))
	})
}

// findMutable is findItem for what has a description to strip: skills and agents.
func findMutable(r *report.Report, name string) ([]report.Row, error) {
	return findPreferring(r, name, func(row report.Row) bool {
		return row.Category == scan.CatSkill || row.Category == scan.CatAgent
	})
}

// findPreferring looks among the removable rows first, then among all of them.
func findPreferring(r *report.Report, name string, eligible func(report.Row) bool) ([]report.Row, error) {
	rows, err := findRow(r, name, func(row report.Row) bool { return row.Removable && eligible(row) })
	if err != nil || len(rows) > 0 {
		return rows, err
	}
	return findRow(r, name, eligible)
}

// findRow reads a typed name: a path to the item, or its invocation key with an
// optional platform and category in front ("codex:skill:plan"), or the bare
// suffix of a namespaced key ("plan" for "ecc:plan"). One name can be several
// items, and which one is not this tool's to guess, so several matches come back
// as an error listing how to name each.
func findRow(r *report.Report, typed string, eligible func(report.Row) bool) ([]report.Row, error) {
	plat, cat, name := splitSelector(typed)
	var exact, leaf []report.Row
	for _, row := range r.Rows {
		if !eligible(row) || (plat != "" && row.Platform != plat) || (cat != "" && string(row.Category) != cat) {
			continue
		}
		switch i := strings.LastIndexByte(row.Name, ':'); {
		case row.Path == typed, row.Name == name:
			exact = append(exact, row)
		case i >= 0 && row.Name[i+1:] == name:
			leaf = append(leaf, row)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = leaf
	}
	if len(matches) < 2 {
		return matches, nil
	}
	// A shared folder is listed once per tool reading it, and it is one item.
	if matches[0].Path != "" && !slices.ContainsFunc(matches, func(m report.Row) bool { return m.Path != matches[0].Path }) {
		return matches[:1], nil
	}
	lines := make([]string, len(matches))
	for i, m := range matches {
		lines[i] = fmt.Sprintf("  %s:%s:%s  %s", m.Platform, m.Category, m.Name, m.Path)
	}
	return nil, fmt.Errorf("%s matches %d items; name one of them, by this form or by its path:\n%s", typed, len(matches), strings.Join(lines, "\n"))
}

// splitSelector takes a known platform and a known category off the front of a
// typed name. Anything else stays part of the name.
func splitSelector(typed string) (plat, cat, name string) {
	name = typed
	if head, rest, ok := strings.Cut(name, ":"); ok {
		for _, p := range platform.All() {
			if string(p.ID) == head {
				plat, name = head, rest
				break
			}
		}
	}
	if head, rest, ok := strings.Cut(name, ":"); ok {
		for _, c := range []scan.Category{scan.CatSkill, scan.CatAgent, scan.CatMCP} {
			if string(c) == head {
				cat, name = head, rest
				break
			}
		}
	}
	return plat, cat, name
}

// exitNotDone is the status of a question not answered yes: nothing ran, which a
// wrapping script has to be able to tell from a run that did.
const exitNotDone = 3

// openTTY is the terminal to ask on when stdin is something else. It is a variable
// so a test can stand in for having no terminal.
var openTTY = func() (*os.File, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }

// answerSource is where a yes may come from: the person at the terminal, never a
// line of data. A loop's list on stdin is that loop's data, and a line in it
// reading "yes" is not consent. With no terminal there is nobody to ask.
func answerSource(stdin io.Reader) (io.Reader, func()) {
	f, isFile := tty.FileOf(stdin)
	if !isFile {
		return stdin, func() {} // an in-process caller's own reader
	}
	if tty.IsTerminal(f) {
		return f, func() {}
	}
	t, err := openTTY()
	if err != nil {
		return nil, func() {}
	}
	return t, func() { t.Close() }
}

// confirm prints prompt to stderr and reads a yes/no answer from the terminal.
// Only y/yes is a yes: a bare Enter is a no, since this guards actions on many
// items. The question goes to stderr so it is on the screen when stdout is
// redirected, and keys pressed before it appeared do not answer it.
func confirm(stdin io.Reader, stderr io.Writer, prompt string) bool {
	fmt.Fprint(stderr, prompt)
	src, done := answerSource(stdin)
	defer done()
	if src == nil {
		fmt.Fprintln(stderr, "\nno terminal to ask on; pass --yes to go ahead")
		return false
	}
	tty.FlushInput(src)
	sc := bufio.NewScanner(src)
	if !sc.Scan() {
		return false
	}
	line := strings.ToLower(strings.TrimSpace(sc.Text()))
	return line == "y" || line == "yes"
}

func cmdInstallHook(opts options, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	if !opts.hasClaudeCode {
		fmt.Fprintln(stderr, "error: the nudge hook is a Claude Code feature, and no Claude Code directory was found")
		return 1
	}
	settings := filepath.Join(opts.claudeDir, "settings.json")
	exe, err := hookExecutable(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
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

// wrapperEnv is how the npm wrapper names itself. The binary it starts lives
// inside the package and is replaced or removed on upgrade, while the wrapper's
// path stays, and the wrapper fetches the binary again when it is missing.
const wrapperEnv = "SKILLREAPER_WRAPPER"

// hookExecutable is the path a hook should call for this install.
func hookExecutable(stderr io.Writer) (string, error) {
	if w := os.Getenv(wrapperEnv); w != "" {
		if strings.Contains(filepath.ToSlash(w), "/_npx/") {
			fmt.Fprintf(stderr, "warning: this is running from npx's cache, which can be cleared at any time, and the hook would then stop working. A permanent install avoids that: npm i -g skillreaper\n")
		}
		return w, nil
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "reap", nil
	}
	return exe, nil
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
	fmt.Fprintf(stderr, "skillreaper: %d %s flagged for pruning since last check. Run `reap` to review.\n", r.DeadCount, report.Plural(r.DeadCount, "skill"))
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
	if !sameItemOnPlatforms(matches) {
		fmt.Fprintf(stderr, "ambiguous: %q matches multiple items:\n", args[0])
		for _, m := range matches {
			fmt.Fprintf(stderr, "  %s\n", report.CanonicalName(m))
		}
		fmt.Fprintln(stderr, "qualify with a category, e.g. skill:<name>")
		return 1
	}

	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	claudeLines := scan.LoadClaudeMD(cwd, home, opts.claudeDir)
	explain := func(row report.Row) report.Explanation {
		muted, _ := mutedSet(opts.claudeDir, []scan.Item{row.Item})
		return report.BuildExplanation(row, r.Sessions, report.ExplainInput{
			MinSessions:   opts.minSessions,
			GraceDays:     opts.graceDays,
			MuteThreshold: opts.muteThreshold,
			WindowDays:    opts.days,
			Muted:         muted[row.Path],
			ClaudeMDRef:   scan.ClaudeMDReferences(claudeLines, row.Name),
			Now:           clock.Now(),
		})
	}
	// One name on several platforms is several items with one answer each, and
	// there is no finer name to ask for: show them all.
	if opts.asJSON {
		var err error
		if len(matches) == 1 {
			err = report.RenderWhyJSON(stdout, explain(matches[0]))
		} else {
			all := make([]report.Explanation, len(matches))
			for i, m := range matches {
				all[i] = explain(m)
			}
			err = report.WriteJSON(stdout, all)
		}
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	for _, m := range matches {
		if len(matches) > 1 {
			fmt.Fprintf(stdout, "%s: %s\n", m.Platform, m.Path)
		}
		report.RenderWhy(stdout, explain(m), colorEnabled(opts, stdout))
	}
	return 0
}

// sameItemOnPlatforms reports rows that differ only in where they live.
func sameItemOnPlatforms(rows []report.Row) bool {
	for _, r := range rows {
		if r.Category != rows[0].Category || r.Name != rows[0].Name {
			return false
		}
	}
	return true
}

func cmdShare(opts options, stdout, stderr io.Writer) int {
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	could := r.DeadTokensPerSession + r.MuteTokensPerSession
	cut := 0
	if entries, err := prune.LoadManifest(opts.claudeDir); err == nil {
		for _, e := range entries {
			if !e.Restored {
				cut += e.Tokens
			}
		}
	}
	switch {
	case opts.asJSON:
		if err := report.RenderShareJSON(stdout, cut, could); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
	case opts.asMarkdown:
		report.RenderShareMarkdown(stdout, cut, could)
	default:
		report.RenderShareText(stdout, cut, could)
	}
	return 0
}

// cmdSnapshot writes this run's report where diff can find it later. It takes
// no measurement of its own: the payload is exactly what --json prints, so a
// snapshot can never disagree with the run that produced it.
func cmdSnapshot(opts options, stdout, stderr io.Writer) int {
	if opts.listKeep {
		saved, err := snapshot.List(opts.claudeDir)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		for _, p := range saved {
			fmt.Fprintln(stdout, snapshot.Name(p))
		}
		return 0
	}
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	path, err := snapshot.Save(opts.claudeDir, r, clock.Now(), snapshotSettings(opts))
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if opts.asJSON {
		if err := report.WriteJSON(stdout, map[string]string{"path": path, "name": snapshot.Name(path)}); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "snapshot written: %s\n", path)
	return 0
}

// snapshotSettings are the options that decide a verdict, kept with a snapshot so
// a diff can say when two were judged differently.
func snapshotSettings(opts options) map[string]string {
	return map[string]string{
		"--days":            strconv.Itoa(opts.days),
		"--min-sessions":    strconv.Itoa(opts.minSessions),
		"--grace-days":      strconv.Itoa(opts.graceDays),
		"--min-tokens":      strconv.Itoa(opts.minTokens),
		"--mute-threshold":  trimFloat(opts.muteThreshold),
		"--mute-min-tokens": strconv.Itoa(opts.muteMinTokens),
	}
}

// cmdDiff compares two snapshots. With no arguments it takes the newest two,
// which is the question people actually have; naming both is for going further
// back. A name is what the diff itself prints, or a path. Snapshots are never
// taken automatically - unasked-for state is the disease this tool treats - so
// an empty history says how to start one rather than reporting nothing.
func cmdDiff(opts options, args []string, stdout, stderr io.Writer) int {
	var from, to string
	if len(args) == 2 {
		from, to = snapshot.Resolve(opts.claudeDir, args[0]), snapshot.Resolve(opts.claudeDir, args[1])
	} else {
		want := 2 - len(args)
		saved, skipped, err := snapshot.Newest(opts.claudeDir, want, clock.Now())
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		for _, msg := range skipped {
			fmt.Fprintf(stderr, "warning: skipped %s\n", msg)
		}
		if len(saved) < want {
			fmt.Fprintf(stderr, "need %d %s to compare, found %d. take one with: reap snapshot\n", want, report.Plural(want, "more snapshot"), len(saved))
			return 1
		}
		if len(args) == 1 {
			from, to = snapshot.Resolve(opts.claudeDir, args[0]), saved[0]
		} else {
			from, to = saved[0], saved[1]
		}
	}

	a, err := snapshot.Load(from)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	b, err := snapshot.Load(to)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	d := snapshot.Compare(a.Report, b.Report, from, to, snapshot.PrunedAt(opts.claudeDir))
	d.Warnings = snapshot.Warnings(a, b)
	if opts.asJSON {
		if err := snapshot.RenderJSON(stdout, d); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}
	snapshot.RenderText(stdout, d)
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
