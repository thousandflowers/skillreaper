package reinvest

import (
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
