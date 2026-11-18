package reinvest

import (
	"os"
	"strings"
	"testing"
)

// Claude Code runs a command on load in two forms: !`cmd` inline, and a
// fenced block opened with three backticks and "!". Only the first was refused.
func TestEnum3_the_fenced_run_on_load_form_is_a_risk(t *testing.T) {
	fence := strings.Repeat("`", 3)
	doc := "---\nname: release-notes-flow\ndescription: d\n---\nSteps.\n" + fence + "!\nopen -a Calculator\n" + fence + "\n"
	if got := strings.Join(risks(doc, false), "; "); !strings.Contains(got, "runs a command as soon as it loads") {
		t.Errorf("the fenced form: %q", got)
	}
	if got := risks("---\nname: x\ndescription: d\n---\n"+fence+"bash\nls\n"+fence+"\n", false); len(got) != 0 {
		t.Errorf("an ordinary code block: %q", got)
	}
}

// "Never beside a skill of that name" goes by the name a skill is called by,
// the one in its header: a skill in folder foo named release-notes-flow did
// not stop a second release-notes-flow from being written.
func TestEnum3_the_name_in_a_skills_header_counts_as_taken(t *testing.T) {
	state, err := parseReapState(`{"Rows": [{"Name": "foo", "FrontmatterName": "release-notes-flow", "Category": "skill", "Path": "/s/foo/SKILL.md"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	taken := canonNames(state.Installed)
	if !taken[canon("release-notes-flow")] || !taken[canon("foo")] {
		t.Errorf("taken: %v", taken)
	}
}

// What goes to Claude has its keys removed, a skill's description too: one
// pasted there went out as written.
func TestEnum3_a_key_in_a_description_is_not_sent(t *testing.T) {
	key := "sk-ant-" + strings.Repeat("a", 40)
	if got := skillLines([]skill{{Name: "x", Desc: "uses " + key + " to call the API"}}, 300); strings.Contains(got, key) || !strings.Contains(got, "uses ") {
		t.Errorf("sent: %q", got)
	}
}

// Before the question the screen says where the skill goes and what is
// muted, by path: it was shown only by --dry-run, and after the apply.
func TestEnum3_the_swap_screen_says_where_and_what(t *testing.T) {
	tempHome(t)
	a := fakeApp(t)
	s := writtenSwap("first")
	s.Mute[0].Path = "/s/first/SKILL.md"
	a.showSwap(s)
	said := termText(a)
	if !strings.Contains(said, "skills-reinvest") || !strings.Contains(said, "reap mute /s/first/SKILL.md") {
		t.Errorf("the screen:\n%s", said)
	}
}

// With --mcp the hosts a server's package and repository live on are asked
// too: the notice named skills.sh and the registry only.
func TestEnum3_the_notice_names_every_host(t *testing.T) {
	if got := noticeFor(options{online: true, mcp: true}); !strings.Contains(got, "npm") || !strings.Contains(got, "GitHub") {
		t.Errorf("notice: %q", got)
	}
}

// A stop after the skill was created says how to take it out again.
func TestEnum3_a_stop_says_how_to_take_the_new_skill_out(t *testing.T) {
	c := candidate{Kind: kindSkill, Name: "ship-release", Slug: "ship-release", Created: true}
	if said := strings.Join(stoppedHalfway(c, false, nil, nil, "", nil), "\n"); !strings.Contains(said, "reap prune ship-release") {
		t.Errorf("stopped after the create:\n%s", said)
	}
	if said := strings.Join(stoppedHalfway(c, true, nil, nil, "", nil), "\n"); strings.Contains(said, "reap prune ship-release") {
		t.Errorf("stopped during the create:\n%s", said)
	}
}

// The skills offered for a mute are the ones reap mute takes without a word:
// not one linked into the folder other tools read (the file edited is theirs
// too, and the screen named the link), not one in a skills folder that is a
// link (reap refuses it, and the same swap was offered every run), not one
// whose path cannot be printed as it is (the undo line did not work).
func TestConfirm_only_what_reap_mutes_plainly_is_offered_for_a_mute(t *testing.T) {
	state, err := parseReapState(`{"Rows": [
		{"Name": "plain", "Category": "skill", "Verdict": "REAP", "Path": "/h/.claude/skills/plain/SKILL.md"},
		{"Name": "shared", "Category": "skill", "Verdict": "REAP", "Path": "/h/.claude/skills/shared/SKILL.md", "LinkTarget": "/h/.agents/skills/shared"},
		{"Name": "infolder", "Category": "skill", "Verdict": "REAP", "Path": "/h/.claude/skills/infolder/SKILL.md", "FolderLink": true},
		{"Name": "vialink", "Category": "skill", "Verdict": "REAP", "Path": "/h/.claude/skills/vialink/SKILL.md", "ViaLink": "/elsewhere/vialink"},
		{"Name": "odd", "Category": "skill", "Verdict": "REAP", "Path": "/h/.claude/skills/old‌five/SKILL.md"}
	]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Installed) != 5 || len(state.Reap) != 1 || state.Reap[0].Name != "plain" {
		t.Errorf("installed %d, offered for a mute: %+v", len(state.Installed), state.Reap)
	}
}

// A skill written for a gap whose name an installed skill has is not
// offered, and says so: it read "no usable answer", or nothing at all, and
// the write was paid for again at every run.
func TestConfirm_a_written_skill_of_a_taken_name_says_so(t *testing.T) {
	tempHome(t)
	b := fakeApp(t)
	alwaysAnswering(b, releaseAnswer())
	first := created(t, b, []kind{releaseGap}, releaseSessions, nil)
	if len(first) != 1 {
		t.Fatalf("the skill was not written: %q", termText(b))
	}
	taken := []skill{{Name: "some-folder", Called: first[0].Slug}}
	if out := created(t, b, []kind{releaseGap}, releaseSessions, taken); len(out) != 0 {
		t.Errorf("offered beside an installed skill of that name: %+v", out)
	}
	if said := termText(b); !strings.Contains(said, "was not offered") || !strings.Contains(said, first[0].Slug) || !strings.Contains(said, "some-folder") {
		t.Errorf("nothing says why:\n%s", said)
	}
}

// A swap rolled back leaves nothing of the folder reinvest made for it.
func TestConfirm_a_rolled_back_swap_leaves_no_empty_folder(t *testing.T) {
	tempHome(t)
	c := candidate{Kind: kindSkill, Name: "ship-release", Slug: "ship-release", Created: true, Doc: writtenDoc}
	if err := create(c); err != nil {
		t.Fatal(err)
	}
	if err := removeWritten(c, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(writtenDir()); err == nil {
		t.Errorf("%s is left behind, empty", writtenDir())
	}
}
