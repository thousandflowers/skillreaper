package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/mute"

	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// putSkill adds a personal skill to a test home, dated long ago.
func putSkill(t *testing.T, home, folder string) string {
	t.Helper()
	p := filepath.Join(home, ".claude", "skills", folder, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\ndescription: some words here that are long enough\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdated(t, p)
	return p
}

func noControlBytes(t *testing.T, what, out string) {
	t.Helper()
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("%s prints a control character from a folder's name: %q", what, out)
	}
}

// A folder's name is not trusted: an escape in it must not reach the terminal.
// The names were cleaned, the paths printed beside them were not.
func TestUnmuteAndRestoreNeverPrintControlCharacters(t *testing.T) {
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	folders := []string{"col\x1b[2Jz", "col\x07[2Jz"}
	paths := make([]string, len(folders))
	for i, folder := range folders {
		paths[i] = putSkill(t, home, folder)
	}

	// Two muted skills whose shown names are one name: the listing adds the paths.
	for _, p := range paths {
		if err := mute.Mute(dir, dir, "col?[2Jz", p); err != nil {
			t.Fatal(err)
		}
	}
	code, out := reapRun("unmute")
	if code != 0 || strings.Count(out, "col?[2Jz") < 2 {
		t.Fatalf("unmute listing: %d %q", code, out)
	}
	noControlBytes(t, "the unmute listing", out)
	if code, out := reapRun("unmute", "--all", "--yes"); code != 0 {
		t.Fatalf("unmute --all: %d %s", code, out)
	}

	// Both in quarantine under one shown name: restore by name lists where each was.
	for _, p := range paths {
		if code, out := reapRun("prune", p, "--yes"); code != 0 {
			t.Fatalf("prune %q: %d %s", p, code, out)
		}
	}
	code, out = reapRun("restore", "col?[2Jz")
	if code == 0 || !strings.Contains(out, "2 times") {
		t.Fatalf("restore by a name two items share: %d %q", code, out)
	}
	noControlBytes(t, "restore by name", out)

	// What was pruned exists again: restore refuses, and names the path.
	for _, folder := range folders {
		putSkill(t, home, folder)
	}
	code, out = reapRun("restore", "--all", "--yes")
	if code == 0 || !strings.Contains(out, "exists again") {
		t.Fatalf("restore --all onto folders that are back: %d %q", code, out)
	}
	noControlBytes(t, "restore --all", out)
	code, out = reapRun("restore", "001")
	if code == 0 || !strings.Contains(out, "exists again") {
		t.Fatalf("restore 001 onto a folder that is back: %d %q", code, out)
	}
	noControlBytes(t, "restore by id", out)
}

// The way out of a refusal is a command to paste: a kept name with a space is
// one word of it.
func TestRefusingAKeptItemGivesACommandThatCanBePasted(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "sp ace")
	if code, out := reapRun("keep", "sp ace"); code != 0 {
		t.Fatalf("keep: %d %s", code, out)
	}
	for _, verb := range []string{"prune", "mute", "delete"} {
		code, out := reapRun(verb, "sp ace", "--yes")
		if code == 0 || !strings.Contains(out, "run reap keep --remove 'sp ace' first") {
			t.Errorf("%s of a kept item: %d %q", verb, code, out)
		}
	}
	if code, out := reapRun("keep", "--remove", "sp ace"); code != 0 {
		t.Errorf("the command it gives: %d %s", code, out)
	}
}

// --json is accepted with snapshot --list, so the list is JSON.
func TestSnapshotListHonoursJSON(t *testing.T) {
	deleteHome(t)
	for range 2 {
		if code, out := reapRun("snapshot"); code != 0 {
			t.Fatalf("snapshot: %d %s", code, out)
		}
	}
	code, out := reapRun("snapshot", "--list", "--json")
	var names []string
	if err := json.Unmarshal([]byte(out), &names); code != 0 || err != nil || len(names) != 2 {
		t.Fatalf("snapshot --list --json: %d %v %q", code, err, out)
	}
	_, plain := reapRun("snapshot", "--list")
	if plain != strings.Join(names, "\n")+"\n" {
		t.Errorf("the two listings disagree: %q and %q", plain, names)
	}
}

// The restore listing prints each name as the manifest has it. For a server
// that is the key from the config file, kept raw so restore can find it: it is
// cleaned on its way to the screen like every other name.
func TestRestoreListingCleansAServersName(t *testing.T) {
	home := deleteHome(t)
	config := `{"mcpServers":{"srv\u001b[41mBAD":{"command":"x"}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := reapRun("prune", "--yes", "--min-sessions", "1"); code != 0 {
		t.Fatalf("prune: %d %s", code, out)
	}
	code, out := reapRun("restore")
	if code != 0 || !strings.Contains(out, "BAD") {
		t.Fatalf("the server is not in the listing: %d %q", code, out)
	}
	noControlBytes(t, "the restore listing", out)
}

// An id is a number: typed without its zeros it is still that id.
func TestRestoreTakesAnIDWithoutItsZeros(t *testing.T) {
	home := deleteHome(t)
	if code, out := reapRun("prune", "mine", "--yes"); code != 0 || !strings.Contains(out, "001") {
		t.Fatalf("prune: %d %s", code, out)
	}
	if code, out := reapRun("restore", "1"); code != 0 {
		t.Errorf("restore 1: %d %s", code, out)
	}
	if !lexist(filepath.Join(home, ".claude", "skills", "mine", "SKILL.md")) {
		t.Error("the skill is not back")
	}
}

// `reap unmute <path>` takes the path as typed: from inside the skills folder
// that is a relative one, and it named a muted file as "not muted".
func TestUnmuteTakesARelativePath(t *testing.T) {
	home := deleteHome(t)
	if code, out := reapRun("mute", "mine", "--yes"); code != 0 {
		t.Fatalf("mute: %d %s", code, out)
	}
	t.Chdir(filepath.Join(home, ".claude", "skills"))
	for _, typed := range []string{"mine/SKILL.md"} {
		if code, out := reapRun("unmute", typed); code != 0 {
			t.Errorf("unmute %s: %d %s", typed, code, out)
		}
	}
}

// A command printed to be pasted acts on the folder this run acted on. With
// --claude-dir it used to leave the folder out: ids start at 001 in every
// folder, so the pasted `reap restore 001` brought back another folder's item.
func TestPrintedCommandsNameTheFolderWhenItWasNamed(t *testing.T) {
	home := deleteHome(t)
	other := filepath.Join(home, "ot her", ".claude")
	p := filepath.Join(other, "skills", "zeta", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\ndescription: some words here that are long enough\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdated(t, p)
	there := " --claude-dir '" + other + "'"
	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"prune", "zeta", "--yes"}, "Undo anytime: reap restore 001" + there + "\n"},
		{[]string{"restore", "001"}, "restored 001"},
		{[]string{"mute", "zeta", "--yes"}, "Undo: reap unmute zeta" + there + "\n"},
		{[]string{"keep", "zeta"}, "Undo: reap keep --remove skill:zeta" + there + "\n"},
		{[]string{"prune", "zeta", "--yes"}, "run reap keep --remove zeta" + there + " first"},
	} {
		if _, out := reapRun(append([]string{"--claude-dir", other}, step.args...)...); !strings.Contains(out, step.want) {
			t.Errorf("%v does not print %q:\n%s", step.args, step.want, out)
		}
	}
	// In the usual folder a command stays as short as it was.
	if _, out := reapRun("prune", "mine", "--yes"); !strings.Contains(out, "Undo anytime: reap restore 001\n") {
		t.Errorf("the usual folder: %s", out)
	}
}

// mute works on skills and agents. Asked to mute a server it said the server
// "cannot be removed by reap; edit the file by hand", which is neither what was
// asked nor true: reap prune takes it out.
func TestMutingAServerSaysWhatMuteIsFor(t *testing.T) {
	deleteHome(t)
	code, out := reapRun("mute", "srv", "--yes")
	if code == 0 || !strings.Contains(out, "reap prune srv") || strings.Contains(out, "by hand") {
		t.Errorf("mute srv: %d %s", code, out)
	}
}

// --claude-json names the file to read and to edit, with or without
// --claude-dir. Given alone it was dropped without a word: the report read the
// usual file, and `reap prune <server>` edited that one.
func TestClaudeJSONGivenAloneIsTheFileReadAndEdited(t *testing.T) {
	home := deleteHome(t)
	usual := filepath.Join(home, ".claude.json")
	before, err := os.ReadFile(usual)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "other.json")
	if err := os.WriteFile(other, []byte(`{"mcpServers":{"onlyhere":{"command":"x"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := reapRun("--claude-json", other, "prune", "onlyhere", "--yes"); code != 0 {
		t.Errorf("the server in the file that was named: %d %s", code, out)
	}
	if after, _ := os.ReadFile(other); strings.Contains(string(after), "onlyhere") {
		t.Errorf("the named file still holds the server: %s", after)
	}
	if code, out := reapRun("--claude-json", other, "prune", "srv", "--yes"); code == 0 {
		t.Errorf("a server of the usual file was pruned through another file's name: %s", out)
	}
	if after, _ := os.ReadFile(usual); string(after) != string(before) {
		t.Errorf("the usual file was edited:\n%s", after)
	}
}

// One name, one file, two scopes: a server for every project and one for a
// single project. By name they are two items, and which one is not reap's to
// guess: it took the first, for good with delete, and said nothing of the other.
func TestAServerDefinedInTwoScopesIsNotGuessedAt(t *testing.T) {
	home := deleteHome(t)
	config := filepath.Join(home, ".claude.json")
	twice := `{"mcpServers":{"twosrv":{"command":"x"}},"projects":{"/p/a":{"mcpServers":{"twosrv":{"command":"z"}}}}}`
	if err := os.WriteFile(config, []byte(twice), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"delete", "prune"} {
		code, out := reapRun(verb, "twosrv", "--yes")
		if code == 0 || !strings.Contains(out, "2 times") || !strings.Contains(out, "/p/a") {
			t.Errorf("%s twosrv: %d %s", verb, code, out)
		}
	}
	if after, _ := os.ReadFile(config); string(after) != twice {
		t.Errorf("the file changed:\n%s", after)
	}
}

// The undo of install-hook, and delete's word about a copy in quarantine, name
// the folder when it was named, as the other printed commands do.
func TestHookUndoNamesTheFolderWhenItWasNamed(t *testing.T) {
	home := deleteHome(t)
	other := filepath.Join(home, "elsewhere", ".claude")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	code, out := reapRun("--claude-dir", other, "install-hook")
	if code != 0 || !strings.Contains(out, "Undo: reap uninstall-hook --claude-dir "+other+"\n") {
		t.Errorf("install-hook: %d %s", code, out)
	}
}

// An id typed without its zeros is that id only when no item is named so, and
// never an entry already restored. `reap restore 1` with a skill named "1" in
// quarantine answered "entry 001 already restored".
func TestRestoreByNumberPrefersTheItemNamedSo(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "1")
	for _, step := range [][]string{{"prune", "mine", "--yes"}, {"restore", "001"}, {"prune", "skill:1", "--yes"}} {
		if code, out := reapRun(step...); code != 0 {
			t.Fatalf("%v: %d %s", step, code, out)
		}
	}
	code, out := reapRun("restore", "1")
	if code != 0 || !strings.Contains(out, "restored 002 (1)") {
		t.Errorf("restore 1: %d %s", code, out)
	}
	if !lexist(filepath.Join(home, ".claude", "skills", "1", "SKILL.md")) {
		t.Error("the skill named 1 is not back")
	}
}

// A name that starts with a dash goes after "--" in a printed command, the
// folder's flags before it: pasted as it was, `reap unmute -dash` was read as a flag.
func TestPrintedCommandsForANameThatStartsWithADash(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "-dash")
	code, out := reapRun("mute", "--yes", "--", "-dash")
	if code != 0 || !strings.Contains(out, "Undo: reap unmute -- -dash\n") {
		t.Fatalf("mute: %d %s", code, out)
	}
	if code, out := reapRun("unmute", "--", "-dash"); code != 0 {
		t.Errorf("the command it printed: %d %s", code, out)
	}
	dir := filepath.Join(home, ".claude")
	if code, out := reapRun("--claude-dir", dir, "mute", "--yes", "--", "-dash"); code != 0 ||
		!strings.Contains(out, "Undo: reap unmute --claude-dir "+dir+" -- -dash\n") {
		t.Errorf("mute with the folder named: %d %s", code, out)
	}
}

// Run under its other name, every command reap prints carries that name: the
// weekly nudge said "Run `reap`", a command an npm install does not have.
func TestHintsUnderTheOtherNameNeverSayReap(t *testing.T) {
	rewrite := hintRewriter("skillreaper", nil)
	for said, want := range map[string]string{
		"skillreaper: 3 unused items to prune, more than at the last check. Run `reap` to review.\n": "skillreaper: 3 unused items to prune, more than at the last check. Run `skillreaper` to review.\n",
		"  3 more · ~334 tok/session · full list: reap --md\n":                                       "  3 more · ~334 tok/session · full list: skillreaper --md\n",
		"error: no item named x; run reap to list what is there\n":                                   "error: no item named x; run skillreaper to list what is there\n",
		"reaped skill x (id 001)\n":                                                                  "reaped skill x (id 001)\n",
	} {
		if got := string(rewrite([]byte(said))); got != want {
			t.Errorf("%q became %q, want %q", said, got, want)
		}
	}
}

// One problem is said once. The plugin registry is read for skills, for agents
// and for servers, and each reading repeated its warnings: "6 warnings" for two.
func TestAWarningIsSaidOnce(t *testing.T) {
	twice := []scan.Warning{{Path: "/a", Msg: "bad id"}, {Path: "/b", Msg: "bad id"}, {Path: "/a", Msg: "bad id"}, {Path: "/a", Msg: "other"}, {Path: "/a", Msg: "bad id"}}
	once := uniqueWarnings(twice)
	if len(once) != 3 || once[0] != twice[0] || once[1] != twice[1] || once[2] != twice[3] {
		t.Errorf("uniqueWarnings = %+v", once)
	}
}

// A folder named with --claude-dir is Claude Code's, and Claude Code keeps
// transcripts: with none in the window the report says that, not that the tool
// "exposes no session transcripts at all".
func TestANamedFolderWithNoSessionsIsNotATranscriptlessTool(t *testing.T) {
	home := deleteHome(t)
	_, out := reapRun("--claude-dir", filepath.Join(home, ".claude"), "--no-banner")
	if strings.Contains(out, "exposes no session transcripts at all") {
		t.Errorf("the report says Claude Code has no transcripts:\n%s", out)
	}
}

// The parsers count from the start of the day the window opens on, in UTC. The
// parse is cached for the day: counted from the exact minute, a later run the
// same day read the cache of an earlier minute and disagreed with a fresh parse.
func TestTheParseWindowOpensAtTheStartOfADay(t *testing.T) {
	morning := time.Date(2026, 10, 17, 2, 0, 0, 0, time.UTC)
	if got, want := parseCutoff(morning), time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("parseCutoff = %s, want %s", got, want)
	}
	if !parseCutoff(morning).Equal(parseCutoff(morning.Add(20 * time.Hour))) {
		t.Error("two runs on one day count from different instants")
	}
	if parseCutoff(morning).Equal(parseCutoff(morning.Add(23 * time.Hour))) {
		t.Error("the next day counts from the same instant")
	}
}
