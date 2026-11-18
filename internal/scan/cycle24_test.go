package scan

import "testing"

// A one-word name counts where a rules file writes it as a name. Cycle 23
// took the first few ways; a heading of its own, a table cell, a list item
// with a dash, a numbered or ticked item, a link and quotes are names too.
// A sentence's word, a path and "review skills" are not.
func TestAPlainWordIsProtectedWhereItIsWrittenAsAName(t *testing.T) {
	for line, want := range map[string]bool{
		"- **deploy** — ships":          true,
		"- **deploy:** ships":           true,
		"- `deploy`":                    true,
		"/deploy":                       true,
		"run /deploy when done":         true,
		"the Deploy skill":              true,
		"the deploy SKILL":              true,
		"skill: deploy":                 true,
		"la skill deploy":               true,
		"  - deploy: x":                 true,
		"## deploy":                     true,
		"### Deploy":                    true,
		"* deploy - what it does":       true,
		"- deploy — does x":             true,
		"- deploy":                      true,
		"1. deploy: x":                  true,
		"1) deploy: x":                  true,
		"- [ ] deploy: x":               true,
		"> deploy: x":                   true,
		"| deploy | what it does |":     true,
		"[deploy](link)":                true,
		`use "deploy" here`:             true,
		"*deploy*":                      true,
		"see skills/deploy/SKILL.md":    true,
		"always deploy the diff":        false,
		"- always deploy on friday":     false,
		"Always deploy skills at once.": false,
		"Store files in docs/deploy.":   false,
		"always approve/deploy the pr":  false,
		"see src/deploy/ folder":        false,
		"deploy: ship it":               false, // a label in a sentence, no list mark
		"## how to deploy":              false,
		"| we deploy | on friday |":     false,
	} {
		if got := ClaudeMDReferences([]string{line}, "deploy"); got != want {
			t.Errorf("%q: %v, want %v", line, got, want)
		}
	}
}

// The short name of a plugin's skill is read by the same rule as a plain word.
func TestAPluginsShortNameIsReadAsAPlainWordIs(t *testing.T) {
	for line, want := range map[string]bool{"- plan: writes one": true, "## plan": true, "plan the work first": false} {
		if got := ClaudeMDReferences([]string{line}, "ecc:plan"); got != want {
			t.Errorf("%q: %v, want %v", line, got, want)
		}
	}
}

// A word is a word in any alphabet: "révision" in a sentence protected the
// skill of that name, because only ASCII letters were a plain word.
func TestAPlainWordIsOneInAnyAlphabet(t *testing.T) {
	for line, want := range map[string]bool{"faire la révision du code": false, "- révision: relit le diff": true, "`révision`": true} {
		if got := ClaudeMDReferences([]string{line}, "révision"); got != want {
			t.Errorf("%q: %v, want %v", line, got, want)
		}
	}
}

// Mentioned is the name standing alone anywhere, written as a name or not:
// reap why says when a name is there and protects nothing.
func TestClaudeMDMentionsIsTheWordAnywhere(t *testing.T) {
	lines := []string{"always review the diff"}
	if !ClaudeMDMentions(lines, "review") || ClaudeMDMentions(lines, "view") || ClaudeMDMentions(lines, "") {
		t.Error("mentions")
	}
}

// A key of a nested map is not the skill's own: "metadata:\n  allowed-tools"
// counted as its tools, and a description's indented line "tools: a, b" too.
func TestAnIndentedKeyIsNotTheHeadersOwn(t *testing.T) {
	nested := []byte("---\nname: x\ndescription: d\nmetadata:\n  allowed-tools: Read, Write\n  disable-model-invocation: true\n---\n")
	if got := toolSurface(nested, "allowed-tools", "tools"); got != ToolSurfaceAll {
		t.Errorf("tools of a nested key: %d", got)
	}
	if manualOnly(nested) {
		t.Error("manual-only by a nested key")
	}
	own := []byte("---\nname: x\nallowed-tools: Read, Write   \ndisable-model-invocation: true\n---\n")
	if got := toolSurface(own, "allowed-tools"); got != 2 || !manualOnly(own) {
		t.Errorf("its own keys: tools %d, manual %v", got, manualOnly(own))
	}
}
