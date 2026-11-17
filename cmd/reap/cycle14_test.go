package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// reapStdout runs the CLI and returns what it printed for a program to read.
func reapStdout(t *testing.T, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(args, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("reap %v: %d %s", args, code, errOut.String())
	}
	return out.String()
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A server named a__b was used; its transcript is swept and a dozen sessions
// follow that never call it. The record of past uses has to find it: it came
// out REAP, and `reap prune --yes` took a server that had been used.
func TestAUsedServerWithTheSeparatorInItsNameIsNotReapedLater(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"a__b":{"command":"x"}}}`)
	projects := filepath.Join(home, ".claude", "projects", "p")
	stamp := func(hoursAgo int) string {
		return time.Now().Add(-time.Duration(hoursAgo) * time.Hour).UTC().Format(time.RFC3339)
	}
	used := filepath.Join(projects, "used.jsonl")
	mustWriteFile(t, used, `{"type":"assistant","sessionId":"u","timestamp":"`+stamp(48)+`","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__a__b__tool","input":{}}]}}`+"\n")
	verdict := func() string {
		var r struct {
			Rows []struct{ Name, Verdict string }
		}
		if err := json.Unmarshal([]byte(reapStdout(t, "--json", "--no-banner")), &r); err != nil {
			t.Fatal(err)
		}
		for _, row := range r.Rows {
			if row.Name == "a__b" {
				return row.Verdict
			}
		}
		t.Fatal("no row for a__b")
		return ""
	}
	if v := verdict(); v != "KEEP" {
		t.Fatalf("while its transcript is there: %s", v)
	}

	if err := os.Rename(used, filepath.Join(home, "swept.jsonl")); err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		id := "later" + strconv.Itoa(i)
		mustWriteFile(t, filepath.Join(projects, id+".jsonl"), `{"type":"user","sessionId":"`+id+`","timestamp":"`+stamp(1)+`","message":{"content":"hi"}}`+"\n")
	}

	if v := verdict(); v == "REAP" {
		t.Errorf("a server that was used comes out %s once its transcript is gone", v)
	}
}

// --claude-dir on a folder with no config of its own takes no other profile's:
// ~/.claude-work has ~/.claude.json next to it, and that file is ~/.claude's.
func TestANamedFolderDoesNotTakeAnotherProfilesConfig(t *testing.T) {
	home := deleteHome(t)
	work := filepath.Join(home, ".claude-work")
	if err := os.MkdirAll(filepath.Join(work, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".claude.json")
	before := readFile(t, cfg)

	if out := reapStdout(t, "--claude-dir", work, "--json", "--no-banner"); strings.Contains(out, `"Name": "srv"`) {
		t.Error("the report of ~/.claude-work lists a server of ~/.claude.json")
	}
	reapRun("prune", "--yes", "--claude-dir", work, "srv")

	if after := readFile(t, cfg); after != before {
		t.Errorf("~/.claude.json was edited:\n%s", after)
	}
}

// A command inside an error is pasted as it is printed. Ids start at 001 in
// every folder, so without the folder it brings back another folder's item.
func TestCommandsInsideAnErrorNameTheFolder(t *testing.T) {
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	tail := " --claude-dir " + scan.ShellArg(dir)
	in := func(args ...string) string {
		_, out := reapRun(append(args, "--claude-dir", dir)...)
		return out
	}

	in("prune", "--yes", "mine")
	putSkill(t, home, "mine")
	if out := in("restore", "001"); !strings.Contains(out, "reap restore 001"+tail) {
		t.Errorf("restore over a folder that is back:\n%s", out)
	}
	if out := in("restore", "--all", "--yes"); !strings.Contains(out, "reap restore 001"+tail) {
		t.Errorf("restore --all over a folder that is back:\n%s", out)
	}

	in("mute", "--yes", "copied")
	in("prune", "--yes", "copied")
	if out := in("unmute", "copied"); !strings.Contains(out, "reap restore 002"+tail+", then reap unmute copied"+tail) {
		t.Errorf("unmute of a skill in quarantine:\n%s", out)
	}

	if out := in("mute", "srv"); !strings.Contains(out, "reap prune srv"+tail) {
		t.Errorf("mute of a server:\n%s", out)
	}
}

// "codex:foo" can be a name in its own right, and not foo in Codex.
func TestANameThatStartsLikeASelectorIsStillFound(t *testing.T) {
	home := deleteHome(t)
	for _, name := range []string{"codex:foo", "agent:bar"} {
		p := putSkill(t, home, name)
		if code, out := reapRun("prune", "--yes", name); code != 0 || lexist(p) {
			t.Errorf("prune %s: %d\n%s", name, code, out)
		}
	}
}

// The way out of a kept item is printed to be pasted, and has to work for a
// name that starts with a dash too.
func TestTheKeepRemoveHintWorksForANameWithADash(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "-dash")
	if code, out := reapRun("keep", "--", "-dash"); code != 0 {
		t.Fatalf("keep: %d %s", code, out)
	}

	_, out := reapRun("prune", "--yes", "--", "-dash")

	if !strings.Contains(out, "run reap keep --remove skill:-dash first") {
		t.Fatalf("the hint: %s", out)
	}
	if code, out := reapRun("keep", "--remove", "skill:-dash"); code != 0 {
		t.Errorf("the hint, pasted: %d %s", code, out)
	}
}

// What was pruned and then put back by hand saves nothing, and share says so.
func TestShareCountsNothingForAnItemPutBackByHand(t *testing.T) {
	home := deleteHome(t)
	reapRun("prune", "--yes", "mine")
	moved, _ := filepath.Glob(filepath.Join(home, ".claude", "reaped", "skill", "*"))
	if len(moved) != 1 {
		t.Fatalf("in quarantine: %v", moved)
	}
	if err := os.Rename(moved[0], filepath.Join(home, ".claude", "skills", "mine")); err != nil {
		t.Fatal(err)
	}

	var said struct {
		Saved int `json:"tokens_saved_per_session"`
	}
	if err := json.Unmarshal([]byte(reapStdout(t, "share", "--json")), &said); err != nil {
		t.Fatal(err)
	}

	if said.Saved != 0 {
		t.Errorf("share claims %d tokens saved for an item that is back in place", said.Saved)
	}
}

// A path that was mistyped is said, not read as an empty stack.
func TestAMistypedPathIsSaid(t *testing.T) {
	home := deleteHome(t)
	if code, out := reapRun("--claude-json", filepath.Join(home, "nope.json"), "--no-banner"); code == 0 || !strings.Contains(out, "nope.json") {
		t.Errorf("--claude-json on a missing file: %d\n%s", code, firstN(out, 300))
	}
	if code, out := reapRun("snapshot", "--list", "--claude-dir", filepath.Join(home, "missing")); code == 0 {
		t.Errorf("snapshot --list on a missing folder: %d\n%s", code, out)
	}
	if code, out := reapRun("--claude-dir", filepath.Join(home, ".claude.json"), "--no-banner"); code == 0 {
		t.Errorf("--claude-dir on a file: %d\n%s", code, firstN(out, 300))
	}
}

// 001 was restored, and something named 001 is in quarantine: that is the one
// the typed 001 can still mean.
func TestRestoreReadsARestoredIdAsAName(t *testing.T) {
	home := deleteHome(t)
	reapRun("prune", "--yes", "mine")
	reapRun("restore", "001")
	p := putSkill(t, home, "001")
	reapRun("prune", "--yes", "001")
	if lexist(p) {
		t.Fatal("the skill named 001 was not pruned")
	}

	code, out := reapRun("restore", "001")

	if code != 0 || !lexist(p) || !strings.Contains(out, "restored 002 (001)") {
		t.Errorf("restore 001: %d\n%s", code, out)
	}
}

// reap why counts an item against the sessions of its own tool, as its verdict
// does: a tool with no session in the window has none, whatever the others have.
func TestWhyCountsTheSessionsOfTheItemsOwnTool(t *testing.T) {
	deleteHome(t)

	_, out := reapRun("why", "dup") // one copy in Claude Code, one in Codex: both are shown

	for _, want := range []string{"1 total in window", "0 total in window"} {
		if !strings.Contains(out, want) {
			t.Errorf("why, with one Claude Code session and no Codex one, lacks %q:\n%s", want, out)
		}
	}
}

// A key written twice in the config: taking the server out takes every spelling
// of it, or the earlier one becomes the live definition.
func TestPruneTakesEverySpellingOfAServerWrittenTwice(t *testing.T) {
	home := deleteHome(t)
	cfg := filepath.Join(home, ".claude.json")
	mustWriteFile(t, cfg, `{"mcpServers":{"srv":{"command":"old"},"other":{"command":"y"},"srv":{"command":"new"}}}`)

	if code, out := reapRun("prune", "--yes", "srv"); code != 0 {
		t.Fatalf("prune: %d %s", code, out)
	}
	if left := readFile(t, cfg); strings.Contains(left, `"srv"`) {
		t.Errorf("srv is still defined: %s", left)
	}
	reapRun("restore", "001")
	var back struct {
		Servers map[string]struct{ Command string } `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(readFile(t, cfg)), &back); err != nil || back.Servers["srv"].Command != "new" {
		t.Errorf("after restore the server in force is %q (%v)", back.Servers["srv"].Command, err)
	}
}

// A flag only reinvest reads, typed with reinvest not first, stops at reap's own
// flag check. The answer still says where reinvest goes.
func TestReinvestAfterAFlagOfItsOwnSaysToPutItFirst(t *testing.T) {
	code, out := reapRun("--days", "5", "reinvest", "--max", "2")

	if code != 2 || !strings.Contains(out, "reinvest reads its own flags, so it comes first: reap reinvest --days 5 --max 2\n") {
		t.Errorf("%d %s", code, out)
	}
}

// usedThenSwept runs the report with one transcript that uses a server, then
// with that transcript gone and a dozen later sessions that never call it, and
// returns the server's verdict each time.
func usedThenSwept(t *testing.T, server, toolUse string) (while, after string) {
	t.Helper()
	home := deleteHome(t)
	cfg, err := json.Marshal(map[string]any{"mcpServers": map[string]any{server: map[string]string{"command": "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(home, ".claude.json"), string(cfg))
	projects := filepath.Join(home, ".claude", "projects", "p")
	stamp := func(hoursAgo int) string {
		return time.Now().Add(-time.Duration(hoursAgo) * time.Hour).UTC().Format(time.RFC3339)
	}
	used := filepath.Join(projects, "used.jsonl")
	mustWriteFile(t, used, `{"type":"assistant","sessionId":"u","timestamp":"`+stamp(48)+`","message":{"content":[`+toolUse+`]}}`+"\n")
	verdict := func() string {
		var r struct {
			Rows []struct{ Name, Verdict string }
		}
		if err := json.Unmarshal([]byte(reapStdout(t, "--json", "--no-banner")), &r); err != nil {
			t.Fatal(err)
		}
		for _, row := range r.Rows {
			if row.Name == server {
				return row.Verdict
			}
		}
		t.Fatalf("no row for %s", server)
		return ""
	}
	while = verdict()
	if err := os.Rename(used, filepath.Join(home, "swept.jsonl")); err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		id := "later" + strconv.Itoa(i)
		mustWriteFile(t, filepath.Join(projects, id+".jsonl"), `{"type":"user","sessionId":"`+id+`","timestamp":"`+stamp(1)+`","message":{"content":"hi"}}`+"\n")
	}
	return while, verdict()
}

// Where a server's name ends is not always in a tool's name. A server that was
// used is seen as used, in the window and in the record of past uses, when its
// name ends in an underscore once cleaned (docs.), is one such character (é),
// or holds "__" and was reached through the resource tools.
func TestAUsedServerWithAnAwkwardNameIsNeverReaped(t *testing.T) {
	call := func(name string) string { return `{"type":"tool_use","id":"t1","name":"` + name + `","input":{}}` }
	for server, use := range map[string]string{
		"docs.": call("mcp__docs___search"),
		"é":     call("mcp_____t"),
		"a__b":  `{"type":"tool_use","id":"t1","name":"ReadMcpResourceTool","input":{"server":"a__b","uri":"x"}}`,
	} {
		t.Run(server, func(t *testing.T) {
			while, after := usedThenSwept(t, server, use)
			if while != "KEEP" {
				t.Errorf("while its transcript is there: %s", while)
			}
			if after == "REAP" {
				t.Errorf("once its transcript is gone: %s", after)
			}
		})
	}
}

// A name is text someone may paste into a Markdown report: what would read as
// a tag or a link there is written so that it reads as the name.
func TestMarkdownReportKeepsANameFromReadingAsMarkup(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "<script>")
	putSkill(t, home, "[link](x)")

	out := reapStdout(t, "--md", "--no-banner")

	for _, raw := range []string{"| <script> |", "| [link](x) |"} {
		if strings.Contains(out, raw) {
			t.Errorf("the Markdown report holds %q", raw)
		}
	}
	for _, safe := range []string{"| &lt;script> |", `| \[link](x) |`} {
		if !strings.Contains(out, safe) {
			t.Errorf("the Markdown report lacks %q:\n%s", safe, out)
		}
	}
}

// The report shows a name with its control characters taken out. reap why
// finds the item by the name as it is on disk too.
func TestWhyFindsAnItemByItsNameOnDisk(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "tab\there")

	for _, typed := range []string{"tab\there", "tab?here"} {
		if code, out := reapRun("why", typed); code != 0 {
			t.Errorf("why %q: %d %s", typed, code, out)
		}
	}
}
