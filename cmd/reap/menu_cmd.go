package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/clock"
	"github.com/thousandflowers/skillreaper/internal/menu"
	"github.com/thousandflowers/skillreaper/internal/mute"
	"github.com/thousandflowers/skillreaper/internal/override"
	"github.com/thousandflowers/skillreaper/internal/prune"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/term"
)

const (
	menuCommand     = "menu"
	reinvestCommand = "reinvest"
	// publicName is the name that opens the menu when typed alone. `reap` alone
	// stays the report, for everything that reads it.
	publicName      = "skillreaper"
	reportName      = "reap"
	disableTimeout  = 60 * time.Second
	maxClaudeJSON   = 64 << 20
	readingNotice   = "reading your stack and your sessions..."
	errOutputHeader = "claude plugin disable: "
)

// typedName is the command as the user typed it. Under the npm wrapper the
// binary carries one name whichever command started it, so the wrapper's own
// path says which of the two it was: `reap` must stay the report there too.
func typedName(arg0, wrapper string) string {
	if wrapper != "" {
		name := strings.TrimSuffix(strings.TrimSuffix(invokedName(wrapper), ".js"), ".cmd")
		if name == publicName || name == reportName {
			return name
		}
	}
	return invokedName(arg0)
}

// opensMenu says a bare `skillreaper` in a terminal means the menu. Any argument,
// a pipe or a terminal that cannot draw one means the report, as before.
func opensMenu(invoked string, args []string) bool {
	return invoked == publicName && len(args) == 0 && term.Problem() == ""
}

// cmdMenu opens the interactive menu. Nothing changes until its receipt is
// answered with y.
func cmdMenu(opts options, stdout, stderr io.Writer) int {
	if why := term.Problem(); why != "" {
		fmt.Fprintf(stderr, "error: cannot open the menu: %s\n", why)
		fmt.Fprintln(stderr, "reap prints the report instead, and reap prune, mute and delete act from the command line.")
		return 1
	}
	if !requireClaudeDir(opts, stderr) {
		return 1
	}
	fmt.Fprintln(stderr, readingNotice)
	data, err := menuData(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	screen, err := term.Open()
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot open the menu: %v\n", err)
		return 1
	}
	defer screen.Close() // a panic in the menu must not leave the shell a raw, blank screen
	applier := &menuApplier{dir: opts.claudeDir, hold: screen.Hold, seen: stampsOf(data.Report.Rows)}
	runMenu(screen, data, applier)
	screen.Close()
	// Only now: a line printed over the menu would be lost with its screen.
	for _, w := range applier.warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	return 0
}

// runMenu shows the menu until it is left. A variable only so a test can make it fail.
var runMenu = func(s term.Screen, d menu.Data, a menu.Applier) { menu.New(s, d, a).Run() }

// menuData is everything the menu shows: the same report `reap` prints, what is
// in quarantine, and how this account is billed.
func menuData(opts options) (menu.Data, error) {
	r, err := gather(opts)
	if err != nil {
		return menu.Data{}, err
	}
	entries, err := prune.LoadManifest(opts.claudeDir)
	if err != nil {
		return menu.Data{}, err
	}
	var pruned []prune.Entry
	for _, e := range entries {
		if !e.Restored && !prune.NeverMoved(e) {
			pruned = append(pruned, e)
		}
	}
	return menu.Data{Report: r, Pruned: pruned, Billing: billingType(opts.claudeJSON), Currency: menu.SystemCurrency(clock.Now())}, nil
}

// billingType reads how the account is billed from Claude's own config. Unknown
// is "", and money is then worded as a floor.
func billingType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var cfg struct {
		OAuthAccount struct {
			BillingType string `json:"billingType"`
		} `json:"oauthAccount"`
	}
	if err := json.NewDecoder(io.LimitReader(f, maxClaudeJSON)).Decode(&cfg); err != nil {
		return ""
	}
	return cfg.OAuthAccount.BillingType
}

// menuApplier carries out what the menu's receipt listed, with the same calls
// and the same refusals the commands make.
type menuApplier struct {
	dir string
	// hold makes Ctrl-C wait for the item in hand; nil where there is no screen.
	hold     func() (release func())
	warnings []string
	// seen is each item's file as the menu's report read it; nil checks nothing.
	seen stamps
}

func (a *menuApplier) Apply(picks []menu.Pick) []menu.Outcome {
	out := make([]menu.Outcome, len(picks))
	for i, pk := range picks {
		out[i].Pick = pk
	}
	if a.hold != nil {
		defer a.hold()()
	}
	// Before the wait for the lock, which can last seconds: a Ctrl-C pressed
	// then must stop the apply, not be lost and let all of it through.
	stopped, release := applyGuard()
	defer release()
	lock, err := lockUnlessStopped(a.dir, stateLockWait, stopped)
	if err != nil {
		switch {
		case stopped():
			err = errors.New("interrupted before it was done")
		case errors.Is(err, prune.ErrLocked):
			err = errors.New("another reap is changing this directory; wait for it to finish")
		}
		for i := range out {
			out[i].Err = err
		}
		return out
	}
	defer lock.Release()

	notes := map[string]prune.Note{}
	disabled := map[string]error{}
	for i, pk := range picks {
		// A plugin is switched off once for all its rows: the rest of them are
		// off already, whatever stopped the run after that.
		if result, done := disabled[pk.Row.Raw().Source]; done && pk.Verb == menu.Disable {
			out[i].Err = result
			continue
		}
		if stopped() {
			out[i].Err = errors.New("interrupted before it was done")
			continue
		}
		out[i].Err = a.one(pk, notes, disabled)
	}
	if err := prune.RecordNotes(a.dir, notes); err != nil && len(notes) > 0 {
		// The items are in quarantine and restorable; only their recorded weight is missing.
		a.warnings = append(a.warnings, fmt.Sprintf("could not record the weights of what was pruned: %v", err))
	}
	return out
}

// guardKept refuses to take away what the keep list protects, as the commands
// do. A list that cannot be read protects everything: nobody knows what is on it.
func (a *menuApplier) guardKept(verb, key string) error {
	if verb != menu.Prune && verb != menu.Delete && verb != menu.Mute {
		return nil
	}
	kept, err := override.KeepSet(a.dir)
	if err != nil {
		return fmt.Errorf("the keep list cannot be read, so nothing it may protect is touched: %w", err)
	}
	if kept[key] {
		return errors.New("it is locked: unlock it first")
	}
	return nil
}

func (a *menuApplier) one(pk menu.Pick, notes map[string]prune.Note, disabled map[string]error) error {
	row := pk.Row.Raw()
	// By the name on disk, which is what the report reads the keep list by: the
	// name shown has its control characters taken out.
	key := override.ItemKey(string(row.Category), row.DiskName())
	if err := a.guardKept(pk.Verb, key); err != nil {
		return err
	}
	switch pk.Verb {
	case menu.Prune:
		var e prune.Entry
		var err error
		if row.Category == scan.CatMCP {
			e, err = prune.RemoveMCP(a.dir, row.Path, serverScope(row), row.DiskName())
		} else {
			e, err = prune.QuarantineChosen(a.dir, row.Item)
		}
		if err == nil {
			notes[e.ID] = prune.Note{Tokens: row.Tokens, Platform: row.Platform, Source: row.Source}
		}
		return ignoreGone(err)
	case menu.Delete:
		if a.seen.changed(row) {
			// No name: the result line gives it. Reopening is the way to see it
			// as it is now; the menu shows what it read at launch.
			return errors.New("changed since the menu read it, and a delete cannot be undone: reopen the menu to delete it")
		}
		if row.Category == scan.CatMCP {
			return ignoreGone(prune.DeleteMCP(a.dir, row.Path, serverScope(row), row.DiskName()))
		}
		return ignoreGone(prune.DeleteItem(row.Item))
	case menu.Mute:
		if err := mute.Mute(a.dir, row.RootDir, row.Name, row.Path); err != nil && !errors.Is(err, mute.ErrAlreadyMuted) && !goneSinceScan(err) {
			return err
		}
		a.seen.refresh(row) // the menu's own rewrite of the file is not a change by someone else
		return nil
	case menu.Unmute:
		// The row's own file first: a name can be muted in several tools at once.
		if _, err := mute.UnmutePath(a.dir, row.Path); !errors.Is(err, mute.ErrNotMuted) {
			return err
		}
		names, err := mute.List(a.dir)
		if err != nil {
			return err
		}
		name, err := resolveMuted(names, row.Name)
		if err != nil {
			return err
		}
		_, err = mute.Unmute(a.dir, name)
		return err
	case menu.Lock:
		return override.AddKeep(a.dir, key)
	case menu.Unlock:
		// Rows of one name share one lock: the first of them takes it off, and
		// for the others it is gone already, which is what was asked for.
		if kept, err := override.KeepSet(a.dir); err == nil && !kept[key] {
			return nil
		}
		_, err := override.RemoveKeep(a.dir, key)
		return err
	case menu.Restore:
		return prune.Restore(a.dir, pk.Row.PruneID)
	case menu.Disable:
		// One command turns the whole plugin off, whichever of its rows comes first.
		if _, done := disabled[row.Source]; !done {
			disabled[row.Source] = disablePlugin(strings.TrimPrefix(row.DiskSource(), "plugin:"))
		}
		return disabled[row.Source]
	}
	return fmt.Errorf("unknown action %q", pk.Verb)
}

// ignoreGone: an item that vanished between the list and the y is not a failure.
// goneSinceScan is an item that was there when the stack was read and is not
// now: another run pruned it meanwhile. It is out of the way, which is no failure.
func goneSinceScan(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func ignoreGone(err error) error {
	if errors.Is(err, prune.ErrAlreadyGone) {
		return nil
	}
	return err
}

// disablePlugin asks Claude Code to turn a plugin off. reap never edits the
// plugin's own files for this: the switch is Claude Code's.
func disablePlugin(id string) error {
	path, err := exec.LookPath("claude")
	if err != nil {
		return errors.New("claude is not on the PATH: run /plugin in Claude Code to disable it")
	}
	ctx, cancel := context.WithTimeout(context.Background(), disableTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "plugin", "disable", id)
	cmd.WaitDelay = 2 * time.Second // a child of claude holding the pipe must not outlive the timeout
	out, err := cmd.CombinedOutput()
	if err != nil {
		last := strings.TrimSpace(string(out))
		if cut := strings.LastIndex(last, "\n"); cut >= 0 {
			last = last[cut+1:]
		}
		// What the command printed is not ours: shown without what would spoof the line.
		return errors.New(errOutputHeader + term.Visible(firstNonEmpty(last, err.Error())))
	}
	return nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
