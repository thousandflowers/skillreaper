// Package reinvest trades skills you never use for ones that do work you do
// and have nothing for.
//
//  1. sessions   the ones you wrote (no `claude -p`, no subagents, not the open one)
//  2. kinds      Claude groups them by kind of work; kinds a skill already covers drop out: what is left are gaps
//  3. write      for each gap Claude writes a skill from your own sessions: the steps you repeat, the rules you
//     had to repeat (with --online: search skills.sh instead, with --mcp the official MCP registry too)
//  4. verify     every (gap, candidate) read in full by Claude, alone; most are rejected, with a reason
//  5. swaps      each fit is paired with REAP skills of about the same weight; pick them with the arrow keys,
//     then reinvest installs the new one and runs `reap mute` on the old ones (reversible)
//
// Weights and verdicts come from `reap --json`, so the numbers match the reap
// report. It is a port of the Python prototype in experiments/reinvest: same
// cache files, same prompts, same checks.
package reinvest

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const (
	defaultDays  = 120
	defaultSwaps = 3
	exitOK       = 0
	exitFailed   = 1
	exitUsage    = 2
	exitSignal   = 130
	usageLine    = "usage: reap reinvest [--dry-run] [--regroup] [--online [--mcp]] [--days N] [--max N]"
)

type options struct {
	days    int
	max     int
	regroup bool
	online  bool
	mcp     bool
	noMCP   bool
	dryRun  bool
}

func newFlagSet(opts *options) *flag.FlagSet {
	fs := flag.NewFlagSet("reap reinvest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.IntVar(&opts.days, "days", defaultDays, "sessions to read (longer than reap's window: recurring work needs history)")
	fs.IntVar(&opts.max, "max", defaultSwaps, "swaps proposed at most")
	fs.BoolVar(&opts.regroup, "regroup", false, "group every session from scratch now, instead of weekly")
	fs.BoolVar(&opts.online, "online", false, "search skills.sh instead of writing skills from your sessions")
	fs.BoolVar(&opts.mcp, "mcp", false, "with --online: also offer MCP servers. Off by default: the registry gives no signal of trust, and a server runs code")
	fs.BoolVar(&opts.noMCP, "no-mcp", false, "")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "pick swaps but only print what would run: installs and mutes nothing")
	return fs
}

func printUsage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(w, usageLine)
	fmt.Fprintln(w, "\nTrades skills you never use for ones that do work you do and have nothing for.")
	fmt.Fprintln(w)
	fs.VisitAll(func(f *flag.Flag) {
		if f.Usage != "" { // a flag with no help is kept for old command lines only
			fmt.Fprintf(w, "  --%-8s %s\n", f.Name, f.Usage)
		}
	})
}

// parseArgs reads the command line. ok is false when Run should stop with code.
func parseArgs(args []string, stdout, stderr io.Writer) (opts options, code int, ok bool) {
	fs := newFlagSet(&opts)
	err := fs.Parse(args)
	if err == nil && fs.NArg() > 0 {
		err = fmt.Errorf("unrecognized arguments: %s", strings.Join(fs.Args(), " "))
	}
	switch {
	case errors.Is(err, flag.ErrHelp):
		printUsage(stdout, fs)
		return opts, exitOK, false
	case err != nil:
		fmt.Fprintln(stderr, usageLine)
		fmt.Fprintf(stderr, "reap reinvest: error: %v\n", err)
		return opts, exitUsage, false
	}
	return opts, exitOK, true
}

// Run is `reap reinvest`. args are what follows the subcommand (--dry-run,
// --regroup, --online, --mcp, --days N, --max N).
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, code, ok := parseArgs(args, stdout, stderr)
	if !ok {
		return code
	}
	err := newApp(stdin, stdout, stderr).run(opts)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errInterrupted):
		return exitSignal
	}
	fmt.Fprintln(stderr, err)
	return exitFailed
}

// outcome is what the pipeline hands to the part that talks to the person.
type outcome struct {
	state    reapState
	gaps     []kind
	swaps    []swap
	rejected []rejection
}

func (a *app) run(opts options) error {
	if err := os.MkdirAll(a.cacheDir, sharedDir); err != nil {
		return err
	}
	log, err := os.Create(a.cachePath(logFile)) // library chatter goes to the log; the terminal gets steps
	if err != nil {
		return err
	}
	a.log = log
	result, err := a.pipeline(opts)
	a.log = io.Discard
	if closeErr := log.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := a.saveProposals(result.swaps); err != nil {
		return err
	}
	return a.show(result, opts.dryRun)
}

// pipeline reads, groups, finds candidates and verifies them.
func (a *app) pipeline(opts options) (outcome, error) {
	a.step("reading sessions", false)
	state, err := a.loadReapState()
	if err != nil {
		return outcome{}, err
	}
	sessions, err := a.rememberSessions(a.readSessions(opts.days, canonNames(state.Installed)))
	if err != nil {
		return outcome{}, err
	}
	a.step(fmt.Sprintf("%d sessions read", len(sessions)), true)

	a.step("grouping your work", false)
	groups, err := a.loadGroups(sessions, opts.regroup)
	if err != nil {
		return outcome{}, err
	}
	kinds := kindsOfWork(groups, sessions)
	gaps, err := a.findGaps(kinds, state.Installed)
	if err != nil {
		return outcome{}, err
	}
	a.step(fmt.Sprintf("%d kinds of work · %d with no skill", len(kinds), len(gaps)), true)

	cands, refused, err := a.candidates(opts, gaps, sessions, state)
	if err != nil {
		return outcome{}, err
	}
	a.step(fmt.Sprintf("verifying %d candidates", len(cands)), false)
	passing, rejected, err := a.verifyAll(cands, state.Used, state.Installed)
	if err != nil {
		return outcome{}, err
	}
	rejected = append(rejected, refusalLines(refused)...) // what the rules turned down before any verifier saw it
	a.step(fmt.Sprintf("%d fit", len(passing)), true)

	if err := a.writeReport(kinds, gaps, passing, rejected); err != nil {
		return outcome{}, err
	}
	settled, err := a.settle(state.MCP, state.Reap)
	if err != nil {
		return outcome{}, err
	}
	pool := state.Reap
	for _, s := range settled {
		pool = withoutSkills(pool, s.Mute)
	}
	swaps := append(settled, build(bestPerGap(passing, opts.max), passing, pool)...)
	return outcome{state: state, gaps: gaps, swaps: swaps, rejected: rejected}, nil
}

type gapCandidates struct {
	cands   []candidate
	refused []refusal
}

// candidates finds what could fill the gaps: skills written from the
// sessions, or with --online what skills.sh and the MCP registry offer.
func (a *app) candidates(opts options, gaps []kind, sessions []session, state reapState) ([]candidate, []refusal, error) {
	if !opts.online {
		a.step(fmt.Sprintf("writing a skill for %d gaps from your sessions", len(gaps)), false)
		cands, refused, err := a.createdFor(gaps, sessions, state.Installed)
		if err != nil {
			return nil, nil, err
		}
		a.step(fmt.Sprintf("%d skills written", len(cands)), true)
		return cands, refused, nil
	}
	a.step("searching skills and MCP servers", false)
	unique := map[string]bool{}
	for _, g := range gaps {
		for _, q := range g.Queries {
			unique[q] = true
		}
	}
	queries := make([]string, 0, len(unique))
	for q := range unique {
		queries = append(queries, q)
	}
	sort.Strings(queries)
	if err := os.MkdirAll(a.cacheDir, sharedDir); err != nil {
		return nil, nil, err
	}
	found := a.searchSkills(queries, canonNames(state.Installed))
	noMCP := opts.noMCP || !opts.mcp
	perGap := parallelMap(gaps, max(1, len(gaps)), func(g kind) gapCandidates {
		cands, refused := a.candidatesFor(g, found, noMCP)
		return gapCandidates{cands, refused}
	})
	var cands []candidate
	var refused []refusal
	for _, got := range perGap {
		cands = append(cands, got.cands...)
		refused = append(refused, got.refused...)
	}
	a.step(fmt.Sprintf("%d candidates", len(cands)), true)
	return cands, refused, nil
}

// show tells the person what was found: the picker on a terminal, the
// same swaps as text otherwise.
func (a *app) show(result outcome, dryRun bool) error {
	fmt.Fprintln(a.term)
	told := a.refusalNotes(result.rejected)
	for _, line := range told {
		fmt.Fprintln(a.term, line)
	}
	switch {
	case len(result.swaps) == 0:
		line := "  no swaps: nothing verified for your gaps"
		if len(told) == 0 {
			line += " · details: " + a.reportPath()
		}
		fmt.Fprintln(a.term, line)
		return nil
	case isTerminal(a.stdin) && a.termTTY:
		err := a.chooseAndApply(result.swaps, dryRun)
		if errors.Is(err, errNoPicker) { // say so, and still show the proposals
			fmt.Fprintln(a.term, a.summary(result.swaps, result.gaps, result.state.Dead))
			fmt.Fprintf(a.stderr, "  %v: the proposals are listed above, nothing was changed\n", err)
			return nil
		}
		return err
	}
	fmt.Fprintln(a.term, a.summary(result.swaps, result.gaps, result.state.Dead))
	return nil
}

const applyQuestion = "  apply? installs the new ones, mutes the old ones (reap unmute undoes) [y/N] "

func (a *app) chooseAndApply(swaps []swap, dryRun bool) error {
	chosen, err := a.pick(swaps)
	if err != nil {
		return err
	}
	if len(chosen) == 0 {
		fmt.Fprintln(a.term, "  nothing accepted · nothing changed")
		return nil
	}
	for _, s := range chosen {
		fmt.Fprintf(a.term, "  %s  in place of  %s\n", named(s.cand()), names(s.Mute))
	}
	if !dryRun {
		fmt.Fprint(a.term, applyQuestion)
		answer, err := readLine(a.stdin)
		if err != nil && err != io.EOF {
			return err
		}
		if pyLower(pyStrip(answer)) != "y" {
			fmt.Fprintln(a.term, "  nothing changed")
			return nil
		}
	}
	fmt.Fprintln(a.term, strings.Join(a.apply(chosen, dryRun), "\n"))
	if dryRun {
		fmt.Fprintln(a.term, "  dry run · nothing installed or muted")
		return nil
	}
	return a.dropProposals(chosen)
}
