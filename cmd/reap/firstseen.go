package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thousandflowers/skillreaper/internal/evidence"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
	"github.com/thousandflowers/skillreaper/internal/usage"
)

// seenKey names an item in the record of when reap first found it. Not by
// its path: a plugin's skill moves to a new folder with every update.
func seenKey(it scan.Item) string {
	return strings.Join([]string{it.Platform, string(it.Category), it.Name, it.Source}, "\x1f")
}

// stampFirstSeen dates the items by when reap first found them in the stack,
// where that is later than the date they carry, and says whether the record
// changed. At the first look everything is simply there (the zero time). An
// item found at a later look is new then: a server added to a config, which
// has no date of its own, or an item that was pruned and restored. A muted
// skill is dated by the last look that found it muted: it has no use because
// of the mute. Judged at once on sessions they were not in, all of these came
// out REAP.
func stampFirstSeen(d *evidence.Digest, items []scan.Item, muted map[string]bool, now time.Time) (changed bool) {
	firstLook := d.FirstSeen == nil
	if firstLook {
		d.FirstSeen = map[string]time.Time{}
		changed = true
	}
	present, scanned := map[string]bool{}, map[string]bool{}
	for i := range items {
		it := &items[i]
		if it.Category != scan.CatSkill && it.Category != scan.CatAgent && it.Category != scan.CatMCP {
			continue
		}
		key := seenKey(*it)
		present[key], scanned[it.Platform] = true, true
		at, known := d.FirstSeen[key]
		if !known && !firstLook || muted[it.Path] {
			at = now
		}
		if !known || !at.Equal(d.FirstSeen[key]) {
			d.FirstSeen[key], changed = at, true
		}
		if at.After(it.InstalledAt) {
			it.InstalledAt = at
		}
	}
	// What is gone from the tools looked at is forgotten: back later, it is new.
	for key := range d.FirstSeen {
		platform, _, _ := strings.Cut(key, "\x1f")
		if scanned[platform] && !present[key] {
			delete(d.FirstSeen, key)
			changed = true
		}
	}
	return changed
}

// folded puts one transcript's uses into the record of past uses and says
// whether it did. A transcript read only in part is left out: marked as read
// there, it was never read again, and what it held was lost when it left the
// window.
func folded(d *evidence.Digest, id, platformID string, one *usage.Stats) bool {
	if one.IncompleteEvidence {
		return false
	}
	d.Add(id, scopedObservation(platformID, observationFrom(one)))
	return true
}

// shortHistoryNote says, above a plan, that it was made on a few days of
// history: under half the window asked for. The report says it; the plan,
// which is what is acted on, did not.
func shortHistoryNote(r *report.Report) string {
	if r.MonthlyBasisDays == 0 || r.MonthlyBasisDays*2 >= r.WindowDays {
		return ""
	}
	return fmt.Sprintf("Judged on %s of history, not the %d asked for: what is used less often than that reads as unused.",
		fmt.Sprintf("%d %s", r.MonthlyBasisDays, report.Plural(r.MonthlyBasisDays, "day")), r.WindowDays)
}

// unreadProtection says why a file that tells what is at work could not be
// read: the settings (the session's agent, the hooks that load skills) or an
// agent's file (the skills it preloads). Empty when all could be, or are not
// there.
func unreadProtection(claudeDir string) string {
	paths := []string{filepath.Join(claudeDir, "settings.json"), filepath.Join(claudeDir, "settings.local.json")}
	agents, _ := filepath.Glob(filepath.Join(claudeDir, "agents", "*.md"))
	for _, path := range append(paths, agents...) {
		f, err := os.Open(path)
		if err == nil {
			_ = f.Close()
			continue
		}
		if !os.IsNotExist(err) {
			return fmt.Sprintf("%s could not be read (%v), and may say that it is at work", shown(path), err)
		}
	}
	return ""
}

// sessionProjects are the folders sessions ran in that are still there.
func sessionProjects(st *usage.Stats) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, path := range st.ProjectPaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() && !seen[path] {
			seen[path] = true
			dirs = append(dirs, path)
		}
	}
	return dirs
}

// projectLoads names what those projects' agents and hooks load, with the
// reason each is kept for.
func projectLoads(projects []string) map[string]string {
	loaded := map[string]string{}
	for _, project := range projects {
		byAgents, byHooks := scan.ProjectLoaded(project)
		for name := range byHooks {
			loaded[name] = report.ReasonHookLoaded
		}
		for name := range byAgents {
			loaded[name] = report.ReasonAgentLoaded
		}
	}
	return loaded
}
