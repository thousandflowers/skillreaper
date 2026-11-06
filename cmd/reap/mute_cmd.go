package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/thousandflowers/skillreaper/internal/mute"
	"github.com/thousandflowers/skillreaper/internal/report"
)

func cmdMute(opts options, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	keepGoingWithoutReader()
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	r, err := gather(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	named := len(args) == 1
	if !keepListReadable(opts, stderr) {
		return 1
	}

	// Named: mutes that skill or agent regardless of verdict (the user asked for
	// it). Bulk: every eligible item. Both rewrite files, so both show the plan
	// and ask, unless --yes.
	var candidates []report.Row
	var skipped []itemProblem
	if named {
		rows, err := findMutable(r, args[0])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		if len(rows) == 0 {
			fmt.Fprintln(stderr, pruneNameMiss(r, args[0]))
			return 1
		}
		if refuseKept(rows[0], stderr) {
			return 1
		}
		candidates = rows
	} else {
		sources := 0
		for _, row := range r.Rows {
			if !report.MuteEligible(row) {
				continue
			}
			sources++
			if opts.source != "" && row.Source != opts.source {
				continue
			}
			// Not eligible, however little it weighs: a file with no description
			// has nothing to strip, and would stop the run at that item for good.
			if !mute.Strippable(row.Path) {
				skipped = append(skipped, problemOf(row, "it has no description to strip"))
				continue
			}
			candidates = append(candidates, row)
		}
		if opts.source != "" && len(candidates) == 0 && sources > 0 {
			fmt.Fprintf(stderr, "error: no items to mute from source %q\n", opts.source)
			return 1
		}
	}

	plan := muteOutcome{candidates: candidates, skipped: skipped}
	for _, row := range candidates {
		plan.planTokens += report.MuteSaving(row)
	}
	machine := opts.asJSON || opts.quiet
	if len(candidates) == 0 {
		if opts.asJSON {
			return plan.writeJSON(stdout, stderr, false)
		}
		if !opts.quiet {
			fmt.Fprintln(stdout, "Nothing to mute.")
		}
		return 0
	}
	if !machine {
		if !named {
			fmt.Fprintf(stdout, "\n%d %s eligible to mute (strips the injected description; reversible):\n", len(candidates), report.Plural(len(candidates), "item"))
			for _, row := range candidates {
				fmt.Fprintf(stdout, "  %-6s  %-40s  ~%d tok/session\n", row.Category, row.Name, report.MuteSaving(row))
			}
		}
	}
	if !opts.yes {
		if machine {
			if opts.asJSON {
				return plan.writeJSON(stdout, stderr, false)
			}
			return 0
		}
		strips := "their descriptions"
		if len(candidates) == 1 {
			strips = "its description"
		}
		prompt := fmt.Sprintf("\nMute %s? This strips %s (reversible). [y/N] ", countAll(len(candidates)), strips)
		if named {
			prompt = fmt.Sprintf("\nMute %s? This strips its description (reversible). [y/N] ", candidates[0].Name)
		}
		if !confirm(stdin, stderr, prompt) {
			fmt.Fprintln(stdout, "aborted")
			return exitNotDone
		}
	}

	stopped, release := applyGuard()
	defer release()
	lock, ok := lockState(opts.claudeDir, stateLockWait, stderr)
	if !ok {
		return 1
	}
	defer lock.Release()

	out := plan
	for i, row := range candidates {
		if stopped() {
			out.interrupted = len(candidates) - i
			break
		}
		err := mute.Mute(opts.claudeDir, row.RootDir, row.Name, row.Path)
		switch {
		case err == nil:
			out.muted = append(out.muted, row)
			out.tokens += report.MuteSaving(row)
			if !machine && !named {
				fmt.Fprintf(stdout, "muted %s (~%d tok/session)\n", row.Name, report.MuteSaving(row))
			}
		case errors.Is(err, mute.ErrAlreadyMuted), errors.Is(err, mute.ErrNoDescription):
			if named {
				fmt.Fprintf(stderr, "error: %v\n", err)
				out.failed = append(out.failed, problemOf(row, err.Error()))
				continue
			}
			out.skipped = append(out.skipped, problemOf(row, err.Error()))
		default:
			fmt.Fprintf(stderr, "error muting %s: %v\n", row.Name, err)
			out.failed = append(out.failed, problemOf(row, err.Error()))
		}
	}
	if opts.asJSON {
		return out.writeJSON(stdout, stderr, true)
	}
	if opts.quiet {
		return out.exitCode()
	}
	out.printSummary(opts, stdout, stderr, r, named)
	return out.exitCode()
}

// muteOutcome is a plan and, once applied, what became of it.
type muteOutcome struct {
	candidates  []report.Row
	planTokens  int
	muted       []report.Row
	tokens      int
	skipped     []itemProblem
	failed      []itemProblem
	interrupted int
}

func (o muteOutcome) exitCode() int {
	switch {
	case o.interrupted > 0:
		return exitInterrupted
	case len(o.failed) > 0:
		return 1
	}
	return 0
}

func (o muteOutcome) printSummary(opts options, stdout, stderr io.Writer, r *report.Report, named bool) {
	for _, p := range o.skipped {
		fmt.Fprintf(stdout, "  (%s skipped — %s)\n", p.Name, p.Reason)
	}
	if o.interrupted > 0 {
		fmt.Fprintf(stderr, "stopped: muted %d, %d not reached\n", len(o.muted), o.interrupted)
	}
	if len(o.muted) == 0 {
		if !named {
			fmt.Fprintln(stdout, "\nNothing was muted.")
		}
		return
	}
	if named {
		row := o.muted[0]
		fmt.Fprintf(stdout, "muted %s — description stripped (~%d tok/session saved)\n", row.Name, report.MuteSaving(row))
		fmt.Fprintf(stdout, "Undo: reap unmute %s\n", row.Name)
		if report.IsPluginSource(row.Source) {
			fmt.Fprintln(stdout, "A plugin update can bring the description back.")
		}
	} else {
		fmt.Fprintf(stdout, "\nmuted %d %s · ~%s tok/session reclaimed\n", len(o.muted), report.Plural(len(o.muted), "item"), report.HumanCount(o.tokens))
	}
	col := colorEnabled(opts, stdout)
	report.RenderValueFeedback(stdout, "muted", len(o.muted), o.tokens, r.SessionsPerMonth, r.PricePerToken*1e6, col)
	tryShowShareHint(opts, stdout, col)
}

// writeJSON emits the plan or result, like prune's.
func (o muteOutcome) writeJSON(w, stderr io.Writer, applied bool) int {
	type item struct {
		Category string `json:"category"`
		Name     string `json:"name"`
		Tokens   int    `json:"tokens"`
		Path     string `json:"path,omitempty"`
	}
	list := func(rows []report.Row) []item {
		out := make([]item, 0, len(rows))
		for _, row := range rows {
			out = append(out, item{string(row.Category), row.Name, report.MuteSaving(row), row.Path})
		}
		return out
	}
	total := o.planTokens
	if applied {
		total = o.tokens
	}
	out := struct {
		Candidates  []item        `json:"candidates"`
		TotalTokens int           `json:"total_tokens"`
		Applied     bool          `json:"applied"`
		Muted       []item        `json:"muted"`
		Skipped     []itemProblem `json:"skipped"`
		Errors      []itemProblem `json:"errors"`
		NotReached  int           `json:"not_reached,omitempty"`
	}{list(o.candidates), total, applied, list(o.muted),
		append([]itemProblem{}, o.skipped...), append([]itemProblem{}, o.failed...), o.interrupted}
	if err := report.WriteJSON(w, out); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return o.exitCode()
}

func cmdUnmute(opts options, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	if !opts.all && len(args) == 0 {
		return listMuted(opts.claudeDir, stdout, stderr)
	}
	// Bulk undo asks, as restore --all does: it rewrites many files.
	if opts.all && !opts.yes {
		if code := listMuted(opts.claudeDir, stdout, stderr); code != 0 {
			return code
		}
		entries, err := mute.Entries(opts.claudeDir)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		if len(entries) == 0 {
			return 0
		}
		if !confirm(stdin, stderr, fmt.Sprintf("\nUnmute %s? [y/N] ", countAll(len(entries)))) {
			fmt.Fprintln(stdout, "aborted")
			return exitNotDone
		}
	}
	lock, ok := lockState(opts.claudeDir, stateLockWait, stderr)
	if !ok {
		return 1
	}
	defer lock.Release()

	var res mute.Result
	var err error
	if opts.all {
		res, err = mute.UnmuteAll(opts.claudeDir)
	} else {
		names, listErr := mute.List(opts.claudeDir)
		if listErr != nil {
			fmt.Fprintf(stderr, "error: %v\n", listErr)
			return 1
		}
		name, resolveErr := resolveMuted(names, args[0])
		if resolveErr != nil {
			fmt.Fprintf(stderr, "error: %v\n", resolveErr)
			return 1
		}
		res, err = mute.Unmute(opts.claudeDir, name)
	}
	for _, e := range res.Cleared {
		fmt.Fprintf(stdout, "%s: its file is gone (moved by a plugin update, or deleted), so there is nothing to unmute. Taken off the list. Its old copy is at %s\n", e.Name, e.Backup)
	}
	switch {
	case opts.all:
		n := len(res.Restored)
		fmt.Fprintf(stdout, "unmuted %d %s\n", n, report.Plural(n, "item"))
	case len(res.Restored) == 1:
		fmt.Fprintf(stdout, "unmuted %s\n", res.Restored[0].Name)
	case len(res.Restored) > 1:
		fmt.Fprintf(stdout, "unmuted %s (%d copies)\n", res.Restored[0].Name, len(res.Restored))
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}
