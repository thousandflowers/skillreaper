package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func cycle19Write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cycle19JSON(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// What follows a script's path in a plugin's hook command is not part of its
// name: a ";" or "&" glued to it left the script unread and its skill REAP.
func TestPluginHookPathEndsAtShellPunctuation(t *testing.T) {
	for _, command := range []string{
		"sh ${CLAUDE_PLUGIN_ROOT}/start.sh; echo done",
		"echo \"$(cat ${CLAUDE_PLUGIN_ROOT}/start.sh)\"",
		"sh ${CLAUDE_PLUGIN_ROOT}/start.sh&",
		"sh ${CLAUDE_PLUGIN_ROOT}/start.sh|tee log",
	} {
		plugin := t.TempDir()
		cycle19Write(t, filepath.Join(plugin, "start.sh"), "cat skills/ps1/SKILL.md\n")
		hooks := cycle19JSON(t, command)
		cycle19Write(t, filepath.Join(plugin, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+hooks+`}]}]}}`)

		if !hookLoadedSkills(plugin)["ps1"] {
			t.Errorf("%s: the script was not read", command)
		}
	}
}

// A quote between the variable and the rest of a path changes nothing in the
// shell, and nothing here.
func TestUserHookPathWithAQuotedVariable(t *testing.T) {
	for _, command := range []string{
		`bash "$HOME"/.claude/hooks/start.sh`,
		`bash "${CLAUDE_CONFIG_DIR}"/hooks/start.sh`,
		`bash '$HOME'/.claude/hooks/start.sh`,
	} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".claude")
		cycle19Write(t, filepath.Join(dir, "hooks", "start.sh"), "cat ~/.claude/skills/hooked/SKILL.md\n")
		quoted := cycle19JSON(t, command)
		cycle19Write(t, filepath.Join(dir, "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+quoted+`}]}]}}`)

		if !userHookLoadedSkills(dir)["hooked"] {
			t.Errorf("%s: the script was not read", command)
		}
	}
}

// A script beside the hook's that is a link to a file (a dotfiles folder) is
// read like the others.
func TestUserHookReadsALinkedScriptBesideIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude")
	cycle19Write(t, filepath.Join(dir, "hooks", "wrap.sh"), "sh ./link.sh\n")
	cycle19Write(t, filepath.Join(home, "dotfiles", "real.sh"), "cat ~/.claude/skills/h-sym/SKILL.md\n")
	if err := os.Symlink(filepath.Join(home, "dotfiles", "real.sh"), filepath.Join(dir, "hooks", "link.sh")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(filepath.Join(home, "dotfiles"), filepath.Join(dir, "hooks", "a-folder")); err != nil {
		t.Skip(err)
	}
	cycle19Write(t, filepath.Join(dir, "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"bash ~/.claude/hooks/wrap.sh"}]}]}}`)

	if !userHookLoadedSkills(dir)["h-sym"] {
		t.Error("the linked script was not read")
	}
}

// A comment after a tab is a comment, as after a space.
func TestFrontmatterCommentAfterATab(t *testing.T) {
	if got := skillAgent([]byte("---\nagent: lonely\t# runs in lonely\n---\n")); got != "lonely" {
		t.Errorf("agent = %q", got)
	}
	if got := frontmatterList([]byte("---\nskills:\n  - one\t# first\n  - two\n---\n"), "skills"); len(got) != 2 || got[0] != "one" {
		t.Errorf("skills = %q", got)
	}
}

// A path in quotes is one word, whatever it holds: a space, a bracket, an "&".
// Cut at those, the script was not found and the skill it loads was REAP.
func TestHookPathInQuotesIsOneWord(t *testing.T) {
	for _, folder := range []string{"my hooks", "my(dir)", "a&b"} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		dir := filepath.Join(home, ".claude")
		cycle19Write(t, filepath.Join(dir, folder, "start.sh"), "cat ~/.claude/skills/plain/SKILL.md\n")
		cycle19Write(t, filepath.Join(dir, "settings.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+cycle19JSON(t, `bash "$HOME/.claude/`+folder+`/start.sh"`)+`}]}]}}`)
		if !userHookLoadedSkills(dir)["plain"] {
			t.Errorf("the user's hook in %q: the script was not read", folder)
		}

		plugin := t.TempDir()
		cycle19Write(t, filepath.Join(plugin, folder, "a.sh"), "cat skills/zed/SKILL.md\n")
		cycle19Write(t, filepath.Join(plugin, "hooks", "hooks.json"), `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":`+cycle19JSON(t, `bash "${CLAUDE_PLUGIN_ROOT}/`+folder+`/a.sh"`)+`}]}]}}`)
		if !hookLoadedSkills(plugin)["zed"] {
			t.Errorf("the plugin's hook in %q: the script was not read", folder)
		}
	}
	if got := shellWords(`a "b c"'d' e  "g`); len(got) != 4 || got[1] != "b cd" || got[3] != "g" {
		t.Errorf("words: %q", got)
	}
}
