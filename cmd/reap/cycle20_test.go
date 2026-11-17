package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/menu"
	"github.com/thousandflowers/skillreaper/internal/report"
	"github.com/thousandflowers/skillreaper/internal/scan"
)

// swapThenYes answers the delete question with y, after putting another folder
// of the same name where the skill was: what a pull or a reinstall does while
// the question waits.
type swapThenYes struct {
	t      *testing.T
	folder string
	done   bool
}

func (s *swapThenYes) Read(p []byte) (int, error) {
	if s.done {
		return 0, io.EOF
	}
	s.done = true
	swapFolder(s.t, s.folder)
	return copy(p, "y\n"), nil
}

func swapFolder(t *testing.T, folder string) {
	t.Helper()
	if err := os.Rename(folder, filepath.Join(t.TempDir(), "the-one-reap-read")); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(folder, "SKILL.md"), "---\nname: mine\ndescription: another skill altogether\n---\nnew work, never shown\n")
}

// Delete cannot be undone, so it is for the item reap read and showed. With
// another one in its place by the time the answer comes, nothing is deleted.
func TestDeleteLeavesAnItemThatChangedSinceItWasRead(t *testing.T) {
	home := deleteHome(t)
	folder := filepath.Join(home, ".claude", "skills", "mine")
	var out, errOut bytes.Buffer

	code := run([]string{"delete", "mine"}, &swapThenYes{t: t, folder: folder}, &out, &errOut)

	if code == 0 || !strings.Contains(errOut.String(), "changed since reap read it") {
		t.Errorf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
	if b, err := os.ReadFile(filepath.Join(folder, "SKILL.md")); err != nil || !strings.Contains(string(b), "never shown") {
		t.Errorf("the folder put there meanwhile is gone or changed: %v", err)
	}
}

// The same from the menu, where the wait is as long as the menu stays open.
func TestMenuDeleteLeavesAnItemThatChangedSinceItWasRead(t *testing.T) {
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	rows := reportRows(t)
	row := menuRow(t, rows, "skill", "mine")
	applier := &menuApplier{dir: dir, seen: stampsOf(rows)}
	swapFolder(t, filepath.Join(dir, "skills", "mine"))

	out := applier.Apply([]menu.Pick{{Row: row, Verb: menu.Delete}})

	// The menu's words: no name (the result line gives it), and what to do here.
	if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "changed since the menu read it") || strings.Contains(out[0].Err.Error(), "mine") {
		t.Errorf("delete of a changed item: %v", out[0].Err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "mine", "SKILL.md")); err != nil {
		t.Errorf("the folder put there meanwhile is gone: %v", err)
	}
	// Another row, untouched, is still deleted.
	other := menuRow(t, rows, "skill", "copied")
	if out := applier.Apply([]menu.Pick{{Row: other, Verb: menu.Delete}}); out[0].Err != nil {
		t.Errorf("delete of an unchanged item: %v", out[0].Err)
	}
}

// A mute list that cannot be read stops mute once, before any plan or
// question, and the report says it cannot tell what is muted.
func TestACorruptMuteListStopsMuteOnce(t *testing.T) {
	home := deleteHome(t)
	state := filepath.Join(home, ".claude", "reaped", "muted", "state.json")
	mustWriteFile(t, state, "{bad")
	var out, errOut bytes.Buffer

	code := run([]string{"mute", "--yes"}, strings.NewReader(""), &out, &errOut)

	if said := out.String() + errOut.String(); code != 1 || strings.Count(said, "invalid character") != 1 || !strings.Contains(said, state) {
		t.Errorf("exit %d:\n%s", code, said)
	}
	out.Reset()
	errOut.Reset()
	run([]string{"--no-banner"}, strings.NewReader(""), &out, &errOut)
	if said := out.String() + errOut.String(); !strings.Contains(said, "mute list") {
		t.Errorf("the report says nothing of the mute list it could not read:\n%s", said)
	}
}

// The weekly reminder counts what reap prune would take: an unused item inside
// a plugin is not "to prune".
func TestNudgeCountsWhatPruneWouldTake(t *testing.T) {
	rows := []report.Row{
		{Item: scan.Item{Name: "a", Removable: true}, Verdict: report.VerdictReap},
		{Item: scan.Item{Name: "p:b", Source: "plugin:p@m"}, Verdict: report.VerdictReap},
		{Item: scan.Item{Name: "c", Removable: true}, Verdict: report.VerdictKeep},
	}
	if got := prunable(rows); got != 1 {
		t.Errorf("%d items to prune, want 1", got)
	}
}

// Under npx the agent view's last line names the command that works there, as
// every other hint does.
func TestTheWarningLineIsRenamedLikeTheOtherHints(t *testing.T) {
	rewrite := hintRewriterAs("skillreaper", "npx skillreaper", nil)
	if got := string(rewrite([]byte("1 warning — run reap for details.\n"))); got != "1 warning — run npx skillreaper for details.\n" {
		t.Errorf("hint: %q", got)
	}
}

// swapForNotes puts a folder with no SKILL.md where the skill was: what is
// there now is somebody's folder, and not the item reap read.
type swapForNotes struct {
	t      *testing.T
	folder string
	done   bool
}

func (s *swapForNotes) Read(p []byte) (int, error) {
	if s.done {
		return 0, io.EOF
	}
	s.done = true
	if err := os.Rename(s.folder, filepath.Join(s.t.TempDir(), "the-one-reap-read")); err != nil {
		s.t.Fatal(err)
	}
	mustWriteFile(s.t, filepath.Join(s.folder, "notes.txt"), "my notes\n")
	return copy(p, "y\n"), nil
}

// The check is on what a delete removes, the skill's folder, not only on the
// file reap read in it: another folder of that name, with no SKILL.md, read as
// "gone" and was deleted with what it held.
func TestDeleteLeavesAFolderPutInTheItemsPlace(t *testing.T) {
	home := deleteHome(t)
	folder := filepath.Join(home, ".claude", "skills", "mine")
	var out, errOut bytes.Buffer

	code := run([]string{"delete", "mine"}, &swapForNotes{t: t, folder: folder}, &out, &errOut)

	if code == 0 || !strings.Contains(errOut.String(), "changed since reap read it") {
		t.Errorf("exit %d:\n%s%s", code, out.String(), errOut.String())
	}
	if _, err := os.Stat(filepath.Join(folder, "notes.txt")); err != nil {
		t.Errorf("the folder put there meanwhile is gone: %v", err)
	}
}

// In --json a weight that was never measured is told apart from zero.
func TestPruneJSONTellsAnUnmeasuredWeightFromZero(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"zzsrv":{"command":"x"}}}`)
	var out, errOut bytes.Buffer

	run([]string{"prune", "zzsrv", "--json"}, strings.NewReader(""), &out, &errOut)

	if said := out.String(); !strings.Contains(said, `"weight_unknown": true`) || !strings.Contains(said, `"unmeasured": 1`) {
		t.Errorf("prune --json:\n%s%s", said, errOut.String())
	}
}

// NaN is not "below 0": the message says what it is.
func TestPriceNaNIsCalledNotANumber(t *testing.T) {
	deleteHome(t)
	var out, errOut bytes.Buffer
	run([]string{"--price", "NaN"}, strings.NewReader(""), &out, &errOut)
	if said := errOut.String(); !strings.Contains(said, "--price must be a number") || strings.Contains(said, "at least 0") {
		t.Errorf("--price NaN:\n%s", said)
	}
}

// The menu's own mute rewrites the file: a delete of the row beside it that
// shares that file is not "changed by someone else".
func TestMenuDeleteAfterItsOwnMuteOfTheSameFile(t *testing.T) {
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	rows := reportRows(t)
	row := menuRow(t, rows, "skill", "mine")
	applier := &menuApplier{dir: dir, seen: stampsOf(rows)}

	if out := applier.Apply([]menu.Pick{{Row: row, Verb: menu.Mute}}); out[0].Err != nil {
		t.Fatalf("mute: %v", out[0].Err)
	}
	if out := applier.Apply([]menu.Pick{{Row: row, Verb: menu.Delete}}); out[0].Err != nil {
		t.Errorf("delete after the menu's own mute: %v", out[0].Err)
	}
}

// One day is "1 day" in the warnings too.
func TestWarningsCountOneDayAsOne(t *testing.T) {
	deleteHome(t)
	var out, errOut bytes.Buffer
	run([]string{"--days", "1", "--no-banner"}, strings.NewReader(""), &out, &errOut)
	if said := out.String() + errOut.String(); strings.Contains(said, "1 days") {
		t.Errorf("--days 1:\n%s", said)
	}
}

// rowOf finds a scanned row by tool, category and name.
func rowOf(t *testing.T, rows []report.Row, tool, category, name string) *menu.Row {
	t.Helper()
	for _, r := range rows {
		if string(r.Category) == category && r.Name == name && r.Platform == tool {
			return menu.NewRow(r)
		}
	}
	t.Fatalf("no %s %s named %s in the report", tool, category, name)
	return nil
}

// One file can be two rows: a tool's own folder, and a link to it in another
// tool's. What the menu itself did to the file through one row is not a change
// by someone else when the other row is deleted.
func TestMenuDeleteOfARowWhoseFileTheMenuChangedThroughAnother(t *testing.T) {
	for _, verb := range []string{menu.Mute, menu.Prune} {
		home := deleteHome(t)
		dir := filepath.Join(home, ".claude")
		rows := reportRows(t)
		applier := &menuApplier{dir: dir, seen: stampsOf(rows)}

		if out := applier.Apply([]menu.Pick{{Row: rowOf(t, rows, "codex", "skill", "shared"), Verb: verb}}); out[0].Err != nil {
			t.Fatalf("%s of the codex row: %v", verb, out[0].Err)
		}
		if out := applier.Apply([]menu.Pick{{Row: rowOf(t, rows, "claude-code", "skill", "shared"), Verb: menu.Delete}}); out[0].Err != nil {
			t.Errorf("delete of the link after the menu's own %s: %v", verb, out[0].Err)
		}
	}
}

// addThenYes drops a file into the skill's folder while the question waits.
type addThenYes struct {
	t      *testing.T
	folder string
	done   bool
}

func (s *addThenYes) Read(p []byte) (int, error) {
	if s.done {
		return 0, io.EOF
	}
	s.done = true
	mustWriteFile(s.t, filepath.Join(s.folder, "sub", "notes.txt"), "precious\n")
	return copy(p, "y\n"), nil
}

// A delete takes the skill's whole folder, so the question says so, and a file
// put in the folder after reap read it stops the delete like a changed
// SKILL.md does.
func TestDeleteSaysItTakesTheFolderAndNoticesAFileAddedToIt(t *testing.T) {
	home := deleteHome(t)
	folder := filepath.Join(home, ".claude", "skills", "mine")
	var out, errOut bytes.Buffer

	code := run([]string{"delete", "mine"}, &addThenYes{t: t, folder: folder}, &out, &errOut)

	if said := out.String() + errOut.String(); !strings.Contains(said, "its whole folder, 2 files") {
		t.Errorf("the question does not say what goes:\n%s", said)
	}
	if code == 0 || !strings.Contains(errOut.String(), "changed since reap read it") {
		t.Errorf("exit %d:\n%s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(folder, "sub", "notes.txt")); err != nil {
		t.Errorf("the file added meanwhile is gone: %v", err)
	}
}

// The plan's weight column says "?" for a weight that was not measured, not
// for every server: a measured one shows its tokens, as the total counts them.
func TestPrunePlanShowsAMeasuredServerWeight(t *testing.T) {
	var b bytes.Buffer
	rows := []report.Row{
		{Item: scan.Item{Category: scan.CatMCP, Name: "srv1", Removable: true}, Verdict: report.VerdictReap, Tokens: 17},
		{Item: scan.Item{Category: scan.CatMCP, Name: "srv2", Removable: true}, Verdict: report.VerdictReap, WeightUnknown: true},
	}
	printPrunePlan(options{}, &b, &report.Report{}, rows, rows, false)
	said := b.String()
	if !strings.Contains(said, "~17 tok") || strings.Count(said, "?") != 1 {
		t.Errorf("plan:\n%s", said)
	}
}

// The keep list is printed like every other name: without control characters.
func TestKeepListPrintsNoControlCharacters(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude", "reaped", "overrides.json"), `{"keep":["skill:ev\u001b[31mil"]}`)
	for _, args := range [][]string{{"keep", "--list"}, {"keep", "--remove", "zzz-not-kept"}} {
		var out, errOut bytes.Buffer
		run(args, strings.NewReader(""), &out, &errOut)
		if said := out.String() + errOut.String(); strings.Contains(said, "\x1b") || !strings.Contains(said, "31mil") {
			t.Errorf("%v:\n%q", args, said)
		}
	}
}

// After a report nothing has been saved yet: the star line says what reap can
// save. After a prune it says what that prune saved.
func TestTheStarLineSaysSavedOnlyAfterAPrune(t *testing.T) {
	home := deleteHome(t)
	r := &report.Report{DeadTokensPerSession: 22700}
	var after, afterPrune bytes.Buffer
	tryShowStarCta(options{claudeDir: filepath.Join(home, ".claude")}, &after, r, true)
	if said := after.String(); !strings.Contains(said, "reap can save you ~22.7k tok/session") || strings.Contains(said, "saved you") {
		t.Errorf("after a report: %q", said)
	}
	other := deleteHome(t)
	tryShowStarCta(options{claudeDir: filepath.Join(other, ".claude")}, &afterPrune, r, true, 1300)
	if said := afterPrune.String(); !strings.Contains(said, "reap saved you ~1.3k tok/session") {
		t.Errorf("after a prune: %q", said)
	}
}

// A skill reached through a link that is the whole skills folder is refused
// before anything is promised about it: the note said "the link goes".
func TestAFolderLinkIsRefusedBeforeItsNote(t *testing.T) {
	home := deleteHome(t)
	skills := filepath.Join(home, ".claude", "skills")
	moved := filepath.Join(home, "ext2", "skills")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(skills, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, skills); err != nil {
		t.Skip(err)
	}
	for _, verb := range []string{"delete", "prune"} {
		var out, errOut bytes.Buffer
		code := run([]string{verb, "mine", "--yes"}, strings.NewReader(""), &out, &errOut)
		if said := out.String(); code == 0 || strings.Contains(said, "the link goes") || strings.Contains(said, "reclaim") || !strings.Contains(errOut.String(), "whole skills folder") {
			t.Errorf("%s, exit %d:\n%s%s", verb, code, said, errOut.String())
		}
	}
}

// Names are printed without their control characters here too.
func TestKeepRemoveAndApmPrintNamesSafely(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude", "reaped", "overrides.json"), `{"keep":["skill:ev\u001b[31mil"]}`)
	var out, errOut bytes.Buffer
	run([]string{"keep", "--remove", "skill:ev?[31mil"}, strings.NewReader(""), &out, &errOut)
	if said := out.String() + errOut.String(); strings.Contains(said, "\x1b") || !strings.Contains(said, "removed keep") {
		t.Errorf("keep --remove:\n%q", said)
	}
	var yaml, md bytes.Buffer
	m := report.APMReport{Repo: "nl\ndir \x1b[31m", WindowDays: 30, Sessions: 3}
	report.RenderAPMYAML(&yaml, m)
	report.RenderAPMMarkdown(&md, m)
	for name, said := range map[string]string{"yaml": yaml.String(), "markdown": md.String()} {
		if strings.Contains(said, "\x1b") || strings.Contains(said, "# repo: nl\n") || strings.Contains(said, "proposal — nl\n") {
			t.Errorf("%s:\n%q", name, said)
		}
	}
}

// After an apply, --json's "unmeasured" is of what was moved, as total_tokens is.
func TestPruneJSONCountsUnmeasuredAmongWhatMoved(t *testing.T) {
	var b, errOut bytes.Buffer
	o := pruneOutcome{
		candidates:  []report.Row{{Item: scan.Item{Category: scan.CatSkill, Name: "a"}, Tokens: 34}, {Item: scan.Item{Category: scan.CatMCP, Name: "srv"}, WeightUnknown: true}},
		movedTokens: 34,
	}
	o.writeJSON(&b, &errOut, true)
	if strings.Contains(b.String(), `"unmeasured"`) {
		t.Errorf("an unmeasured server that was not moved is counted:\n%s", b.String())
	}
}

// The same for mute: quiet without --yes is "not done", and says so on stderr.
func TestMuteQuietWithoutYesIsNotDone(t *testing.T) {
	home := deleteHome(t)
	for i := range 12 {
		mustWriteFile(t, filepath.Join(home, ".claude", "projects", "p", "q"+strings.Repeat("x", i)+".jsonl"),
			`{"type":"user","sessionId":"q`+strings.Repeat("x", i)+`","timestamp":"`+nowStamp()+`","message":{"content":"hi"}}`+"\n")
	}
	var out, errOut bytes.Buffer

	code := run([]string{"mute", "mine", "--quiet"}, strings.NewReader(""), &out, &errOut)

	if code != exitNotDone || out.Len() != 0 || !strings.Contains(errOut.String(), "nothing done: --quiet needs --yes") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}

// A named mute of a skill reached through a link outside the folders reap
// edits is refused before any question, in the words reap why uses: it asked
// "Mute x?" first, then answered with an error about roots.
func TestMuteOfALinkedSkillIsRefusedBeforeTheQuestion(t *testing.T) {
	home := deleteHome(t)
	for i := range 12 {
		mustWriteFile(t, filepath.Join(home, ".claude", "projects", "p", "q"+strings.Repeat("x", i)+".jsonl"),
			`{"type":"user","sessionId":"q`+strings.Repeat("x", i)+`","timestamp":"`+nowStamp()+`","message":{"content":"hi"}}`+"\n")
	}
	var out, errOut bytes.Buffer

	code := run([]string{"mute", "ext"}, strings.NewReader("y\n"), &out, &errOut)

	if said := out.String() + errOut.String(); code == 0 || strings.Contains(said, "[y/N]") || strings.Contains(said, "roots") || !strings.Contains(said, "reap mute does not edit files there") {
		t.Errorf("exit %d:\n%s", code, said)
	}
}

// The other way round too: the menu mutes through the link, then the row of
// the real folder is deleted. The folder's listing changed, by the menu's hand.
func TestMenuDeleteOfTheRealFolderAfterAMuteThroughItsLink(t *testing.T) {
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	rows := reportRows(t)
	applier := &menuApplier{dir: dir, seen: stampsOf(rows)}

	if out := applier.Apply([]menu.Pick{{Row: rowOf(t, rows, "claude-code", "skill", "shared"), Verb: menu.Mute}}); out[0].Err != nil {
		t.Fatalf("mute through the link: %v", out[0].Err)
	}
	if out := applier.Apply([]menu.Pick{{Row: rowOf(t, rows, "codex", "skill", "shared"), Verb: menu.Delete}}); out[0].Err != nil {
		t.Errorf("delete of the real folder after the menu's own mute: %v", out[0].Err)
	}
}

// A clock pinned by SOURCE_DATE_EPOCH is said when the evidence shows it is
// wrong for this stack: a transcript newer than it, or a date in the future.
// A shell that exports it (nix) measured every age against 1980, in silence.
func TestAPinnedClockThatDisagreesWithTheEvidenceIsSaid(t *testing.T) {
	real := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	newest := real.Add(-time.Hour)
	for name, c := range map[string]struct {
		pinned time.Time
		warn   bool
	}{
		"long before the newest transcript": {time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC), true},
		"in the future":                     {real.AddDate(0, 1, 0), true},
		"a capture's clock, after its data": {newest.Add(2 * time.Hour), false},
		"within a day of the data":          {newest.Add(-3 * time.Hour), false},
	} {
		w, ok := pinnedClockWarning(c.pinned, real, newest, "/x/.claude")
		if ok != c.warn {
			t.Errorf("%s: warned %v, want %v", name, ok, c.warn)
		}
		if ok && (!strings.Contains(w.Msg, "SOURCE_DATE_EPOCH") || !strings.Contains(w.Msg, c.pinned.Format("2006-01-02")) || !w.Advisory) {
			t.Errorf("%s: %+v", name, w)
		}
	}
	if _, ok := pinnedClockWarning(time.Time{}, real, newest, "/x"); ok {
		t.Error("no pin, and a warning")
	}
}

// The menu's own mute must not take somebody else's change for its own: a
// file put in the folder, or the skill rewritten, before the mute, is still a
// change the person never saw when the delete comes.
func TestMenuDeleteAfterItsOwnMuteStillRefusesAForeignChange(t *testing.T) {
	for name, foreign := range map[string]func(t *testing.T, folder string){
		"a file put in the folder": func(t *testing.T, folder string) {
			if err := os.WriteFile(filepath.Join(folder, "foreign.txt"), []byte("theirs"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"the skill rewritten": func(t *testing.T, folder string) {
			if err := os.WriteFile(filepath.Join(folder, "SKILL.md"), []byte("---\nname: shared\ndescription: another skill altogether\n---\ntheirs\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := deleteHome(t)
			rows := reportRows(t)
			applier := &menuApplier{dir: filepath.Join(home, ".claude"), seen: stampsOf(rows)}
			folder := filepath.Join(home, ".agents/skills/shared")
			foreign(t, folder)

			if out := applier.Apply([]menu.Pick{{Row: rowOf(t, rows, "claude-code", "skill", "shared"), Verb: menu.Mute}}); out[0].Err != nil {
				t.Fatalf("mute through the link: %v", out[0].Err)
			}
			out := applier.Apply([]menu.Pick{{Row: rowOf(t, rows, "codex", "skill", "shared"), Verb: menu.Delete}})

			if out[0].Err == nil || !strings.Contains(out[0].Err.Error(), "changed since the menu read it") {
				t.Errorf("the delete went through: %v", out[0].Err)
			}
			if _, err := os.Stat(filepath.Join(folder, "SKILL.md")); err != nil {
				t.Errorf("the folder was deleted: %v", err)
			}
		})
	}
}
