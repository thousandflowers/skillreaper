package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/thousandflowers/skillreaper/internal/override"
	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// itemProblem is one item a run left alone, and why.
type itemProblem struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
}

func problemOf(row report.Row, reason string) itemProblem {
	return itemProblem{Category: string(row.Category), Name: row.Name, Reason: reason}
}

// keepListReadable stops a bulk action when the keep list cannot be read: without
// it every kept item looks unprotected, and protection has to fail closed.
func keepListReadable(opts options, stderr io.Writer) bool {
	if _, err := override.KeepSet(opts.claudeDir); err != nil {
		fmt.Fprintf(stderr, "error: the keep list at %s cannot be read: %v\nNothing was changed. Fix or remove that file, then run this again.\n", override.Path(opts.claudeDir), err)
		return false
	}
	return true
}

// countAll reads "all 3 items", or "the 1 item".
func countAll(n int) string {
	if n == 1 {
		return "the 1 item"
	}
	return fmt.Sprintf("all %d items", n)
}

func itOrThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// refuseKept stops a named action on a kept item, --yes included: the keep list
// says never, and undoing that is a separate, deliberate step.
func refuseKept(row report.Row, stderr io.Writer) bool {
	if !row.Kept {
		return false
	}
	fmt.Fprintf(stderr, "error: %s is kept: run reap keep --remove %s first\n", row.Name, row.Name)
	return true
}

// serverScope is the project an MCP row's server belongs to, "" for top level.
func serverScope(row report.Row) string {
	if project, ok := strings.CutPrefix(row.Source, "project:"); ok {
		return project
	}
	return ""
}

// unremovableNote says what to do about an unused item prune cannot take. Only a
// plugin's items are disabled through /plugin; anything else is one edit away in
// the file that declares it.
func unremovableNotes(rows []report.Row) []string {
	plugin, other := 0, map[string][]string{}
	var paths []string
	var named []string
	for _, row := range rows {
		if report.IsPluginSource(row.Source) && row.ViaLink == "" {
			plugin++
			continue
		}
		if row.ViaLink != "" || row.Category == scan.CatSkill {
			named = append(named, row.Name)
			continue
		}
		if _, seen := other[row.Path]; !seen {
			paths = append(paths, row.Path)
		}
		other[row.Path] = append(other[row.Path], row.Name)
	}
	var notes []string
	if plugin > 0 {
		notes = append(notes, fmt.Sprintf("%d unused plugin %s skipped — disable via /plugin, or name one to prune it", plugin, report.Plural(plugin, "item")))
	}
	if len(named) > 0 {
		notes = append(notes, fmt.Sprintf("%s skipped — a link or a shared folder, pruned only by name", strings.Join(named, ", ")))
	}
	for _, p := range paths {
		notes = append(notes, fmt.Sprintf("%s skipped — reap cannot edit %s, remove by hand", strings.Join(other[p], ", "), p))
	}
	return notes
}

func cmdPrune(opts options, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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

	var candidates, dead, notRemovable []report.Row
	var refused []itemProblem
	if named {
		rows, err := findItem(r, args[0])
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
	}
	for _, row := range r.Rows {
		if named || row.Verdict != report.VerdictReap {
			continue
		}
		// dead is every unused item, removable or not; candidates is what this
		// run will act on. The by-source summary is built from dead on purpose:
		// most of the weight is usually plugin items that reap cannot remove,
		// and a summary of only the removable ones would show three small
		// sources while the plugin holding 90% of the tokens went unmentioned.
		dead = append(dead, row)
		if !row.Removable {
			notRemovable = append(notRemovable, row)
			continue
		}
		if opts.source != "" && row.Source != opts.source {
			continue
		}
		// A config this tool cannot edit is known now, so the count in the
		// question below is one it can keep.
		if row.Category == scan.CatMCP {
			err := prune.CheckMCP(opts.claudeDir, row.Path, serverScope(row), row.Name)
			if err != nil && !errors.Is(err, prune.ErrAlreadyGone) {
				refused = append(refused, problemOf(row, err.Error()))
				continue
			}
		}
		candidates = append(candidates, row)
	}

	if opts.source != "" && len(candidates) == 0 && len(dead) > 0 {
		fmt.Fprintf(stderr, "error: no removable unused items from source %q\n", opts.source)
		fmt.Fprintln(stderr, "sources with unused items:")
		for _, t := range report.SourceTotals(dead) {
			fmt.Fprintf(stderr, "  %s — %d of %d removable\n", t.Source, t.Removable, t.Items)
		}
		return 1
	}

	// --json and --quiet are non-interactive by definition: a confirmation
	// prompt cannot be answered on a stream someone is piping into jq, and it
	// cannot even be read on one they silenced. Both therefore describe the plan
	// and stop unless --yes was given as well.
	machine := opts.asJSON || opts.quiet
	plan := pruneOutcome{candidates: candidates, notRemovable: notRemovable, refused: refused}
	for _, row := range candidates {
		plan.planTokens += row.Tokens
	}

	if len(candidates) == 0 {
		switch {
		case opts.asJSON:
			return plan.writeJSON(stdout, stderr, false)
		case opts.quiet:
			return 0
		}
		fmt.Fprintln(stdout, "Nothing to reap. Your stack is clean (or evidence is insufficient).")
		printNotes(stdout, "", notRemovable, refused)
		if r.MutedCount > 0 {
			// Muted items are not gone, and not "clean": they are still there and
			// unused, and could be pruned once unmuted.
			fmt.Fprintf(stdout, "%d %s muted, not pruned: reap unmute lists them.\n", r.MutedCount, report.Plural(r.MutedCount, "item"))
		}
		return 0
	}

	if !machine {
		printPrunePlan(opts, stdout, r, candidates, dead, named)
		printNotes(stdout, "\n", notRemovable, refused)
		if named {
			printChosenNote(stdout, candidates[0])
		}
	}

	if !opts.yes {
		if machine {
			// Nothing was touched: report the plan and leave the tree alone.
			if opts.asJSON {
				return plan.writeJSON(stdout, stderr, false)
			}
			return 0
		}
		prompt := fmt.Sprintf("\nPrune %s? This quarantines %s (reversible). [y/N] ", countAll(len(candidates)), itOrThem(len(candidates)))
		if named {
			prompt = fmt.Sprintf("\nPrune %s? This quarantines it (reversible). [y/N] ", candidates[0].Name)
		}
		if opts.source != "" {
			// Naming the source in the prompt matters more than usual: the
			// number is smaller than the one the report showed, and without
			// the reason for that a smaller number reads like a miscount.
			prompt = fmt.Sprintf("\nPrune %d %s from %s? This quarantines %s (reversible). [y/N] ",
				len(candidates), report.Plural(len(candidates), "item"), opts.source, itOrThem(len(candidates)))
		}
		if !confirm(stdin, stderr, prompt) {
			fmt.Fprintln(stdout, "aborted")
			return exitNotDone
		}
	}

	// Serialise against other runs on this directory. Quarantine is a
	// read-modify-write of the manifest, so two runs finishing at once leave
	// one set of moved files with no manifest entry — present in reaped/ and
	// invisible to restore. Measured before this lock existed: 501 skills in,
	// 148 restored after six concurrent runs.
	stopped, release := applyGuard()
	defer release()
	lock, ok := lockState(opts.claudeDir, 0, stderr)
	if !ok {
		return 1
	}
	defer lock.Release()

	out := plan
	notes := map[string]prune.Note{}
	for i, row := range candidates {
		if stopped() {
			out.interrupted = len(candidates) - i
			break
		}
		var e prune.Entry
		var err error
		if row.Category == scan.CatMCP {
			e, err = prune.RemoveMCP(opts.claudeDir, row.Path, serverScope(row), row.Name)
		} else if named {
			e, err = prune.QuarantineChosen(opts.claudeDir, row.Item)
		} else {
			e, err = prune.QuarantineItem(opts.claudeDir, row.Item)
		}
		switch {
		case err == nil:
			out.done = append(out.done, e)
			out.movedTokens += row.Tokens
			notes[e.ID] = prune.Note{Tokens: row.Tokens, Platform: row.Platform, Source: row.Source}
			if !machine {
				fmt.Fprintf(stdout, "reaped %s %s (id %s)\n", row.Category, row.Name, e.ID)
			}
		case errors.Is(err, prune.ErrAlreadyGone):
			// An item that vanished between the report and the move is not a
			// failure of this run: a concurrent prune, or a manual removal,
			// got there first.
			out.gone = append(out.gone, problemOf(row, "it was already gone"))
		default:
			// One item that cannot move says nothing about the rest. The run goes
			// on and the summary names it.
			fmt.Fprintf(stderr, "error reaping %s: %v\n", row.Name, err)
			out.failed = append(out.failed, problemOf(row, err.Error()))
		}
	}
	if err := prune.RecordNotes(opts.claudeDir, notes); err != nil {
		fmt.Fprintf(stderr, "warning: could not record the weights of what was pruned: %v\n", err)
	}
	if opts.asJSON {
		return out.writeJSON(stdout, stderr, true)
	}
	if opts.quiet {
		return out.exitCode()
	}
	return out.printSummary(opts, stdout, stderr, r, named)
}

// printChosenNote says what taking a row no bulk run would take means.
func printChosenNote(stdout io.Writer, row report.Row) {
	switch {
	case row.ViaLink != "":
		fmt.Fprintf(stdout, "  This is a link: the link goes, and what it points to stays at %s\n", row.ViaLink)
	case report.IsPluginSource(row.Source):
		fmt.Fprintf(stdout, "  This is part of %s: an update can bring it back.\n", strings.TrimPrefix(row.Source, "plugin:"))
	case !row.Removable && row.LinkTarget == "":
		fmt.Fprintf(stdout, "  This is a shared folder: other tools using it lose it too.\n")
	}
}

// printPrunePlan shows what the run proposes.
func printPrunePlan(opts options, stdout io.Writer, r *report.Report, candidates, dead []report.Row, named bool) {
	total := 0
	for _, row := range candidates {
		total += row.Tokens
	}
	if named {
		fmt.Fprintf(stdout, "\n🧹  reap says %s (%s) · reclaim ~%s tok/session\n\n",
			candidates[0].Verdict, candidates[0].Reason, report.HumanNum(total))
	} else {
		fmt.Fprintf(stdout, "\n🧹  %d %s unused · reclaim ~%s tok/session\n\n", len(candidates), report.Plural(len(candidates), "item"), report.HumanCount(total))
	}
	// A list of N dead items reads as N decisions. Grouped by where they
	// came from it usually is not: one plugin tends to account for most of
	// the weight, so the real choice is between a handful of sources.
	// Printed only when there is more than one, and only when the run is
	// not already scoped to a single source.
	if opts.source == "" {
		report.RenderSourceTotals(stdout, report.SourceTotals(dead), len(dead))
	}
	for _, row := range candidates {
		weight := fmt.Sprintf("~%d tok", row.Tokens)
		if row.Category == scan.CatMCP || row.Category == scan.CatHook {
			weight = "?"
		}
		note := ""
		if row.Written() {
			// Written by reinvest, with no other copy: prune deletes it,
			// folder included (still restorable from the quarantine).
			note = "  ✎ written by reinvest · deleted with its folder"
		}
		fmt.Fprintf(stdout, "  %-6s  %-40s  %s%s\n", row.Category, row.Name, weight, note)
	}
}

func printNotes(stdout io.Writer, lead string, notRemovable []report.Row, refused []itemProblem) {
	notes := unremovableNotes(notRemovable)
	for _, p := range refused {
		notes = append(notes, fmt.Sprintf("%s skipped — %s", p.Name, p.Reason))
	}
	if len(notes) > 0 {
		fmt.Fprint(stdout, lead)
	}
	for _, n := range notes {
		fmt.Fprintf(stdout, "  (%s)\n", n)
	}
}

// pruneOutcome is a plan and, once applied, what became of it.
type pruneOutcome struct {
	candidates   []report.Row
	notRemovable []report.Row
	refused      []itemProblem
	planTokens   int

	done        []prune.Entry
	movedTokens int
	gone        []itemProblem
	failed      []itemProblem
	interrupted int // items not reached because of a signal
}

func (o pruneOutcome) exitCode() int {
	switch {
	case o.interrupted > 0:
		return exitInterrupted
	case len(o.failed) > 0:
		return 1
	}
	return 0
}

func (o pruneOutcome) printSummary(opts options, stdout, stderr io.Writer, r *report.Report, named bool) int {
	if len(o.gone) > 0 {
		fmt.Fprintf(stderr, "skipped %d %s already gone (e.g. %s); moved or removed since the scan\n",
			len(o.gone), report.Plural(len(o.gone), "item"), o.gone[0].Name)
	}
	if len(o.failed) > 0 {
		fmt.Fprintf(stderr, "%d %s could not be pruned, see above\n", len(o.failed), report.Plural(len(o.failed), "item"))
	}
	if o.interrupted > 0 {
		fmt.Fprintf(stderr, "stopped: pruned %d, %d not reached\n", len(o.done), o.interrupted)
	}
	switch {
	case len(o.done) == 0:
		fmt.Fprintln(stdout, "\nNothing was pruned.")
		return o.exitCode()
	case named && len(o.done) == 1:
		fmt.Fprintf(stdout, "\nDone. Undo anytime: reap restore %s\n", o.done[0].ID)
	default:
		fmt.Fprintf(stdout, "\nDone. Undo anytime: reap restore --all (or a single id)\n")
	}
	col := colorEnabled(opts, stdout)
	// What moved, not what was planned: the saving claimed has to be the saving
	// actually made.
	report.RenderValueFeedback(stdout, "pruned", len(o.done), o.movedTokens, r.SessionsPerMonth, r.PricePerToken*1e6, col)
	tryShowShareHint(opts, stdout, col)
	tryShowStarCta(opts, stdout, r, col)
	return o.exitCode()
}

// writeJSON emits the plan, or its result when --yes carried it out, as a single
// JSON document. Applied is explicit rather than implied by a non-empty entries
// list: a plan over zero candidates and a completed prune of zero candidates are
// different facts, and a caller checking whether its disk changed should not have
// to infer which one it got. A partial run still prints: its caller needs to know
// what moved.
func (o pruneOutcome) writeJSON(w, stderr io.Writer, applied bool) int {
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
	plugin := 0
	for _, row := range o.notRemovable {
		if report.IsPluginSource(row.Source) {
			plugin++
		}
	}
	total := o.planTokens
	if applied {
		total = o.movedTokens
	}
	out := struct {
		Candidates         []item        `json:"candidates"`
		TotalTokens        int           `json:"total_tokens"`
		SkippedPluginItems int           `json:"skipped_plugin_items"`
		Applied            bool          `json:"applied"`
		Pruned             []entry       `json:"pruned"`
		Skipped            []itemProblem `json:"skipped"`
		Errors             []itemProblem `json:"errors"`
		NotReached         int           `json:"not_reached,omitempty"`
	}{
		Candidates:         make([]item, 0, len(o.candidates)),
		TotalTokens:        total,
		SkippedPluginItems: plugin,
		Applied:            applied,
		Pruned:             make([]entry, 0, len(o.done)),
		Skipped:            append(append([]itemProblem{}, o.gone...), o.refused...),
		Errors:             append([]itemProblem{}, o.failed...),
		NotReached:         o.interrupted,
	}
	for _, row := range o.notRemovable {
		switch {
		case report.IsPluginSource(row.Source):
		case row.ViaLink != "" || row.Category == scan.CatSkill:
			out.Skipped = append(out.Skipped, problemOf(row, "a link or a shared folder, pruned only by name"))
		default:
			out.Skipped = append(out.Skipped, problemOf(row, "reap cannot edit "+row.Path+"; remove by hand"))
		}
	}
	for _, row := range o.candidates {
		out.Candidates = append(out.Candidates, item{
			Category: string(row.Category), Name: row.Name, Tokens: row.Tokens, Path: row.Path,
		})
	}
	for _, e := range o.done {
		out.Pruned = append(out.Pruned, entry{ID: e.ID, Name: e.Name, Category: e.Category})
	}
	if err := report.WriteJSON(w, out); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return o.exitCode()
}

// pruneNameMiss says why a name cannot be pruned or muted. Only an item with a
// plugin source is said to be inside a plugin.
func pruneNameMiss(r *report.Report, name string) string {
	for _, row := range r.Rows {
		if row.Name != name && !strings.HasSuffix(row.Name, ":"+name) {
			continue
		}
		if report.IsPluginSource(row.Source) {
			return fmt.Sprintf("error: %s is inside a plugin and can only be disabled via /plugin in Claude Code", row.Name)
		}
		return fmt.Sprintf("error: %s %s cannot be removed by reap; edit %s by hand", row.Category, row.Name, row.Path)
	}
	return fmt.Sprintf("error: no skill, agent or MCP server found: %s", name)
}

func cmdRestore(opts options, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	if !opts.all && len(args) == 1 {
		lock, ok := lockState(opts.claudeDir, stateLockWait, stderr)
		if !ok {
			return 1
		}
		defer lock.Release()
		id, err := restoreID(opts.claudeDir, args[0])
		if err == nil {
			err = prune.Restore(opts.claudeDir, id)
		}
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "restored %s\n", args[0])
		return 0
	}
	count, err := showPruned(opts.claudeDir, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if count == 0 && !opts.all {
		return 0
	}
	if !opts.all {
		fmt.Fprintln(stdout, "\nreap restore <id> brings one back, reap restore --all brings back all of them.")
		return 0
	}
	if count > 0 && !opts.yes && !confirm(stdin, stderr, fmt.Sprintf("\nRestore %s? [y/N] ", countAll(count))) {
		fmt.Fprintln(stdout, "aborted")
		return exitNotDone
	}
	lock, ok := lockState(opts.claudeDir, stateLockWait, stderr)
	if !ok {
		return 1
	}
	defer lock.Release()
	n, err := prune.RestoreAll(opts.claudeDir)
	var partial *prune.RestoreError
	if errors.As(err, &partial) {
		fmt.Fprintf(stdout, "restored %d %s, %d left:\n", n, report.Plural(n, "item"), len(partial.Failed))
		for _, f := range partial.Failed {
			fmt.Fprintf(stderr, "  %s %s (%s): %v\n", f.Entry.Category, f.Entry.Name, f.Entry.ID, f.Err)
		}
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if count > 0 {
		fmt.Fprintf(stdout, "restored %d %s\n", n, report.Plural(n, "item"))
	}
	return 0
}

// restoreID reads what was typed as a manifest id, or as the name of something in
// quarantine, the same short form prune takes. Several copies of a name list their
// ids, since which one to bring back is not this tool's to guess.
func restoreID(claudeDir, arg string) (string, error) {
	entries, err := prune.LoadManifest(claudeDir)
	if err != nil {
		return "", err
	}
	var matches []prune.Entry
	for _, e := range entries {
		if e.ID == arg {
			return arg, nil
		}
		if !e.Restored && !prune.NeverMoved(e) && (e.Name == arg || strings.HasSuffix(e.Name, ":"+arg)) {
			matches = append(matches, e)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("nothing in quarantine with id or name %s; reap restore lists what there is", arg)
	case 1:
		return matches[0].ID, nil
	}
	ids := make([]string, len(matches))
	for i, e := range matches {
		ids[i] = e.ID + " (" + e.From + e.ConfigPath + ")"
	}
	return "", fmt.Errorf("%s is in quarantine %d times; use an id: %s", arg, len(matches), strings.Join(ids, ", "))
}
