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

// verdictsAfter writes a dozen sessions in which nothing is used and returns
// every row's verdict and reason by name.
func verdictsAfter(t *testing.T, home string, extra ...string) (verdict, reason map[string]string) {
	t.Helper()
	projects := filepath.Join(home, ".claude", "projects", "p")
	at := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	for i := range 12 {
		id := "quiet" + strconv.Itoa(i)
		mustWriteFile(t, filepath.Join(projects, id+".jsonl"), `{"type":"user","sessionId":"`+id+`","timestamp":"`+at+`","message":{"content":"hi"}}`+"\n")
	}
	for i, line := range extra {
		mustWriteFile(t, filepath.Join(projects, "extra"+strconv.Itoa(i)+".jsonl"), line+"\n")
	}
	var r struct {
		Rows []struct{ Name, Verdict, Reason string }
	}
	if err := json.Unmarshal([]byte(reapStdout(t, "--json", "--no-banner")), &r); err != nil {
		t.Fatal(err)
	}
	verdict, reason = map[string]string{}, map[string]string{}
	for _, row := range r.Rows {
		verdict[row.Name], reason[row.Name] = row.Verdict, row.Reason
	}
	return verdict, reason
}

// An agent can name skills in its frontmatter (`skills:`), and Claude Code
// loads them into it when it starts. No transcript records that as a use, so
// such a skill read as never used: REAP, and the agent lost what it knew.
func TestASkillAnAgentPreloadsIsNotReaped(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "preloaded")
	putSkill(t, home, "listed-inline")
	putSkill(t, home, "zzqx-unnamed")
	agent := filepath.Join(home, ".claude", "agents", "reviewer.md")
	mustWriteFile(t, agent, "---\nname: reviewer\ndescription: reviews code\nskills:\n  - preloaded\n---\nbody\n")
	backdated(t, agent)
	other := filepath.Join(home, ".claude", "agents", "second.md")
	mustWriteFile(t, other, "---\nname: second\ndescription: another one\nskills: [Listed-Inline, \"quoted\"]\n---\nbody\n")
	backdated(t, other)

	verdict, reason := verdictsAfter(t, home)

	for _, name := range []string{"preloaded", "listed-inline"} {
		if verdict[name] != "KEEP" || reason[name] != "agent-loaded" {
			t.Errorf("%s: %s(%s), want KEEP(agent-loaded)", name, verdict[name], reason[name])
		}
	}
	if verdict["zzqx-unnamed"] != "REAP" {
		t.Errorf("a skill no agent names: %s(%s), want REAP", verdict["zzqx-unnamed"], reason["zzqx-unnamed"])
	}
	if _, out := reapRun("why", "preloaded"); !strings.Contains(out, "an agent names it under `skills:`") {
		t.Errorf("why does not say what holds it:\n%s", out)
	}
}

// A server's prompts are run as /mcp__server__prompt. That is a use of the
// server: filed as a skill of that odd name, the server read as never used.
func TestAServerUsedThroughItsPromptsIsUsed(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"srv-prompt":{"command":"x"},"idle":{"command":"y"}}}`)
	at := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	prompt := `{"type":"user","sessionId":"withprompt","timestamp":"` + at + `","message":{"content":"<command-message>mcp__srv-prompt__review is running…</command-message>\n<command-name>/mcp__srv-prompt__review</command-name>\n<command-args></command-args>"}}`

	verdict, _ := verdictsAfter(t, home, prompt)

	if verdict["srv-prompt"] != "KEEP" || verdict["idle"] != "REAP" {
		t.Errorf("srv-prompt %s, idle %s; want KEEP and REAP", verdict["srv-prompt"], verdict["idle"])
	}
	if _, listed := verdict["mcp__srv-prompt__review"]; listed {
		t.Error("the prompt is listed as an item of its own")
	}
}

// Two servers, one named like the start of the other. The calls of a__b are not
// uses of a, in the window or in the record of past uses: a stays condemnable.
func TestAServerIsNotKeptByAnotherOnesCalls(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"a":{"command":"x"},"a__b":{"command":"y"},"docs":{"command":"z"},"docs.":{"command":"w"}}}`)
	projects := filepath.Join(home, ".claude", "projects", "p")
	at := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	used := filepath.Join(projects, "used.jsonl")
	mustWriteFile(t, used, `{"type":"assistant","sessionId":"u","timestamp":"`+at+`","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__a__b__tool","input":{}},{"type":"tool_use","id":"t2","name":"mcp__docs___search","input":{}}]}}`+"\n")
	reapStdout(t, "--json", "--no-banner") // the record of past uses is written
	if err := os.Rename(used, filepath.Join(home, "swept.jsonl")); err != nil {
		t.Fatal(err)
	}

	verdict, reason := verdictsAfter(t, home)

	for _, name := range []string{"a", "docs"} {
		if verdict[name] != "REAP" {
			t.Errorf("%s, never called: %s(%s), want REAP", name, verdict[name], reason[name])
		}
	}
	for _, name := range []string{"a__b", "docs."} {
		if verdict[name] == "REAP" {
			t.Errorf("%s, called: %s(%s)", name, verdict[name], reason[name])
		}
	}
}

// reap keep writes the key the report reads the keep list by: the name on
// disk. For a name the report shows cleaned, the cleaned name is what gets
// typed, and it said "excluded from prune" for a key nothing read.
func TestKeepByTheNameAsShownProtectsTheItemOnDisk(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "ab\x1b[31mcd")
	kept := func() bool {
		for _, r := range reportRows(t) {
			if r.Category == scan.CatSkill && strings.HasPrefix(r.Name, "ab") {
				return r.Kept
			}
		}
		t.Fatal("the skill is not in the report")
		return false
	}

	for _, typed := range []string{"ab?[31mcd", "skill:ab?[31mcd"} {
		if code, out := reapRun("keep", typed); code != 0 || !kept() {
			t.Errorf("keep %s: %d, kept %v\n%s", typed, code, kept(), out)
		}
		if code, out := reapRun("keep", "--remove", typed); code != 0 || kept() {
			t.Errorf("keep --remove %s: %d, kept %v\n%s", typed, code, kept(), out)
		}
	}
}

// The path of a config file is not the name of a server in it. Typed or pasted
// where a name goes, it took the first server of the file: pruned with --yes,
// deleted for good.
func TestAConfigFilesPathIsNotOneOfItsServers(t *testing.T) {
	home := deleteHome(t)
	cfg := filepath.Join(home, ".claude.json")
	before := readFile(t, cfg)

	for _, verb := range []string{"prune", "delete"} {
		if code, out := reapRun(verb, "--yes", cfg); code == 0 || readFile(t, cfg) != before {
			t.Errorf("%s %s: %d\n%s\nthe file is now: %s", verb, cfg, code, out, readFile(t, cfg))
		}
	}
}

// What the list offers to type is one word each, the path too, and for a
// server it offers no path: the file is every server's.
func TestTheListOfASharedNameOffersOnlyWhatWorks(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "srv") // a skill named like the server srv
	putSkill(t, home, "sp ace")
	other := filepath.Join(home, ".codex", "skills", "sp ace", "SKILL.md")
	mustWriteFile(t, other, "---\nname: sp ace\ndescription: some words here\n---\nbody\n")
	backdated(t, other)

	_, out := reapRun("prune", "srv")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "claude-code:mcp:srv") && !strings.Contains(line, "(a server in ") {
			t.Errorf("the server's line offers its file as a name: %s", line)
		}
	}
	if _, out := reapRun("prune", "sp ace"); !strings.Contains(out, "  '"+filepath.Join(home, ".claude", "skills", "sp ace")) {
		t.Errorf("a path with a space is not one word:\n%s", out)
	}
}

// --claude-dir "" is a folder that was named and is empty, as from a script
// with an unset variable. Read as "not given", the command ran on the usual folder.
func TestAnEmptyFolderFlagIsRefused(t *testing.T) {
	home := deleteHome(t)
	for _, flag := range []string{"--claude-dir", "--claude-json"} {
		code, out := reapRun(flag, "", "prune", "--yes")
		if code != 2 || !lexist(filepath.Join(home, ".claude", "skills", "mine", "SKILL.md")) {
			t.Errorf("%s \"\": %d\n%s", flag, code, firstN(out, 300))
		}
	}
}

// A mute stopped because the item was kept meanwhile says so, as prune does.
func TestAMuteStoppedByAKeepSaysWhy(t *testing.T) {
	deleteHome(t)
	var out, errOut bytes.Buffer

	run([]string{"mute", "mine"}, &keepThenYes{t: t, name: "mine"}, &out, &errOut)

	if said := out.String() + errOut.String(); !strings.Contains(said, "put on the keep list while this run was waiting") {
		t.Errorf("the mute says nothing of why it did nothing:\n%s", said)
	}
}
