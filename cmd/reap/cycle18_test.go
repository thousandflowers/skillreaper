package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// More of what a hook or a setting puts to work, and what only looks like it.
func TestHooksAndSettingsHoldWhatTheyLoadAndNothingElse(t *testing.T) {
	home := deleteHome(t)
	claude := filepath.Join(home, ".claude")
	plugin := filepath.Join(claude, "plugins", "cache", "m", "p", "1")
	agent := func(dir, name string) {
		p := filepath.Join(dir, "agents", name+".md")
		mustWriteFile(t, p, "---\nname: "+name+"\ndescription: an agent for one thing\n---\nbody\n")
		backdated(t, p)
	}
	skill := func(folder, front string) {
		p := filepath.Join(claude, "skills", folder, "SKILL.md")
		mustWriteFile(t, p, "---\n"+front+"description: some words here that are long enough\n---\nbody\n")
		backdated(t, p)
	}
	// The agent a plugin's own settings run the session as.
	agent(plugin, "zzboss")
	mustWriteFile(t, filepath.Join(plugin, "settings.json"), `{"agent":"zzboss"}`)
	// settings.local.json has the last word on the session's agent.
	agent(claude, "zzold")
	agent(claude, "zznew")
	mustWriteFile(t, filepath.Join(claude, "settings.local.json"), `{"agent":"zznew"}`)
	// A SessionStart hook whose script calls another beside it, and one named
	// through $CLAUDE_CONFIG_DIR.
	for _, name := range []string{"zzups", "zznested", "zzcfg", "zzrel"} {
		skill(name, "")
	}
	hooks := filepath.Join(claude, "hooks")
	mustWriteFile(t, filepath.Join(hooks, "wrap.sh"), "#!/bin/sh\npython3 \"$HOME/.claude/hooks/inject.py\"\n")
	mustWriteFile(t, filepath.Join(hooks, "inject.py"), "open(home + '/.claude/skills/zznested/SKILL.md')\n")
	mustWriteFile(t, filepath.Join(hooks, "cfg.sh"), "cat \"$CLAUDE_CONFIG_DIR/skills/zzcfg/SKILL.md\"\n")
	mustWriteFile(t, filepath.Join(claude, "settings.json"), `{"agent":"zzold","hooks":{
		"UserPromptSubmit":[{"hooks":[{"type":"command","command":"cat ~/.claude/skills/zzups/SKILL.md"}]}],
		"SessionStart":[{"hooks":[
			{"type":"command","command":"bash ~/.claude/hooks/wrap.sh"},
			{"type":"command","command":"bash $CLAUDE_CONFIG_DIR/hooks/cfg.sh"},
			{"type":"command","command":"bash ./hooks/rel.sh"}]}]}}`)
	// A path that is relative is the project's, wherever reap happens to run.
	elsewhere := t.TempDir()
	mustWriteFile(t, filepath.Join(elsewhere, "hooks", "rel.sh"), "cat skills/zzrel/SKILL.md\n")
	t.Chdir(elsewhere)
	// A README at a plugin's root that lists its skills is not a hook reading them.
	mustWriteFile(t, filepath.Join(plugin, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"${CLAUDE_PLUGIN_ROOT}/start.sh"}]}]}}`)
	mustWriteFile(t, filepath.Join(plugin, "start.sh"), "#!/bin/sh\necho hello\n")
	mustWriteFile(t, filepath.Join(plugin, "README.md"), "- skills/pl/SKILL.md: what it does\n")
	// An "agent:" inside a description is words, not the skill's agent.
	agent(claude, "zzwords")
	skill("zzdesc", "summary: |\n  agent: zzwords\n")

	verdict, reason := verdictsAfter(t, home)

	for name, want := range map[string]string{
		"p:zzboss": "KEEP(main-agent)", "zznew": "KEEP(main-agent)", "zzold": "REAP(unused)",
		"zznested": "KEEP(hook-loaded)", "zzcfg": "KEEP(hook-loaded)",
		"zzrel": "REAP(unused)", "p:pl": "REAP(unused)", "zzwords": "REAP(unused)",
	} {
		if got := verdict[name] + "(" + reason[name] + ")"; got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
}

// "Nothing to reap" beside a report that could not read everything is not
// "clean": the prune says the report has something to tell.
func TestNothingToReapPointsAtTheWarnings(t *testing.T) {
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
	var out, errOut bytes.Buffer

	for _, verb := range []string{"prune", "mute"} {
		out.Reset()
		run([]string{verb, "--yes", "--no-cache"}, strings.NewReader(""), &out, &errOut)

		if !strings.Contains(out.String(), "run reap to see what is held back, and why") {
			t.Errorf("%s: nothing says the report has a warning:\n%s%s", verb, out.String(), errOut.String())
		}
	}
}
