package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Things at work in every session, or whenever something else runs, with no
// use of their own on record. None of them is dead, and each came out REAP.
func TestWhatIsAtWorkWithNoUseOnRecordIsNotReaped(t *testing.T) {
	home := deleteHome(t)
	claude := filepath.Join(home, ".claude")
	agent := func(name, front string) {
		p := filepath.Join(claude, "agents", name+".md")
		mustWriteFile(t, p, "---\nname: "+name+"\ndescription: an agent for one thing\n"+front+"---\nbody\n")
		backdated(t, p)
	}
	skill := func(folder, front string) {
		p := filepath.Join(claude, "skills", folder, "SKILL.md")
		mustWriteFile(t, p, "---\n"+front+"description: some words here that are long enough\n---\nbody\n")
		backdated(t, p)
	}
	agent("zzmain", "") // the session's own agent, named in settings
	agent("zzfork", "") // the agent a skill runs in
	skill("zzforky", "context: fork\nagent: zzfork\n")
	skill("zzfolder", "name: zzalias\n") // preloaded by the name in its frontmatter
	skill("zzpreb", "")                  // preloaded, in a list with a blank line and comments
	skill("zzprec", "")                  // preloaded, in a list in brackets over two lines
	skill("zzpred", "")
	agent("zzlist", "skills: # what it knows\n  - zzalias\n\n  - zzpreb # why\n")
	agent("zzflow", "skills: [zzprec,\n  zzpred] # two\n")
	skill("zzhooked", "")    // read by a SessionStart hook of the person's own
	skill("zzviascript", "") // read by the script that hook runs
	mustWriteFile(t, filepath.Join(claude, "hooks", "start.sh"), "#!/bin/sh\ncat \"$HOME/.claude/skills/zzviascript/SKILL.md\"\n")
	mustWriteFile(t, filepath.Join(claude, "settings.json"), `{"agent":"zzmain","hooks":{"SessionStart":[{"hooks":[
		{"type":"command","command":"cat ~/.claude/skills/zzhooked/SKILL.md"},
		{"type":"command","command":"bash ~/.claude/hooks/start.sh"}]}]}}`)
	agent("zzidleagent", "")
	skill("zzidleskill", "")

	verdict, reason := verdictsAfter(t, home)

	for name, want := range map[string]string{
		"zzmain": "KEEP(main-agent)", "zzfork": "KEEP(skill-agent)",
		"zzfolder": "KEEP(agent-loaded)", "zzpreb": "KEEP(agent-loaded)", "zzprec": "KEEP(agent-loaded)", "zzpred": "KEEP(agent-loaded)",
		"zzhooked": "KEEP(hook-loaded)", "zzviascript": "KEEP(hook-loaded)",
		"zzidleagent": "REAP(unused)", "zzidleskill": "REAP(unused)",
	} {
		if got := verdict[name] + "(" + reason[name] + ")"; got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
}

// A server named "under." is under_ in a tool's name, and its call reads as a
// call of "under". That is this server's use, not a second server seen only in
// transcripts.
func TestAServerEndingInAnUnderscoreMakesNoGhostRow(t *testing.T) {
	home := deleteHome(t)
	mustWriteFile(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"under.":{"command":"x"}}}`)
	call := `{"type":"assistant","sessionId":"u","timestamp":"2026-11-16T10:00:00Z","message":{"content":[{"type":"tool_use","id":"t1","name":"mcp__under___tool","input":{}}]}}`

	verdict, _ := verdictsAfter(t, home, call)

	if _, ghost := verdict["under"]; ghost {
		t.Errorf("a row for a server named under that is not configured: %v", verdict)
	}
}

// A transcript that cannot be opened may hold the use that would have kept an
// item: with one, nothing is REAP or MUTE, as the README says.
func TestAnUnreadableTranscriptCondemnsNothing(t *testing.T) {
	home := deleteHome(t)
	putSkill(t, home, "zzmaybeused")
	locked := filepath.Join(home, ".claude", "projects", "p", "locked.jsonl")
	mustWriteFile(t, locked, `{"type":"user","sessionId":"l","timestamp":"2026-11-16T10:00:00Z","message":{"content":"hi"}}`+"\n")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	if _, err := os.ReadFile(locked); err == nil {
		t.Skip("this user reads the file all the same")
	}

	verdict, reason := verdictsAfter(t, home)

	if verdict["zzmaybeused"] == "REAP" || verdict["zzmaybeused"] == "MUTE" {
		t.Errorf("with a transcript that cannot be read: %s(%s)", verdict["zzmaybeused"], reason["zzmaybeused"])
	}
}

// A prune that leaves an item alone because it was kept meanwhile names it.
func TestAPruneStoppedByAKeepSaysWhy(t *testing.T) {
	deleteHome(t)
	var out, errOut bytes.Buffer

	run([]string{"prune", "mine"}, &keepThenYes{t: t, name: "mine"}, &out, &errOut)

	if said := out.String() + errOut.String(); !strings.Contains(said, "mine was put on the keep list while this run was waiting") {
		t.Errorf("the prune does not name what it left alone:\n%s", said)
	}
}

// A failed scan is said by every command that reads the stack, and a manifest
// is of the item that was named.
func TestAFailedScanIsSaidEverywhereAndAnEmptyNameIsNoItem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".cfg"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	mustWriteFile(t, filepath.Join(home, ".claude.json"), "{not json")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range [][]string{{"route"}, {"by-project"}, {"apm"}, {"mute", "--yes"}} {
		if code, out := reapRun(cmd...); code == 0 || !strings.Contains(out, "A failed scan, not a clean stack.") {
			t.Errorf("reap %v: %d\n%s", cmd, code, firstN(out, 200))
		}
	}
	deleteHome(t)
	if code, out := reapRun("manifest", ""); code == 0 {
		t.Errorf("manifest of an empty name: %d\n%s", code, firstN(out, 200))
	}
}
