package reinvest

import (
	"testing"

	"github.com/thousandflowers/skillreaper/internal/scan"
)

// What reinvest weighs a candidate at is what reap reports once it is
// installed, header quirks included: one reading of the frontmatter, not two.
func TestWeight_agrees_with_reap_on_every_header(t *testing.T) {
	for name, doc := range map[string]string{
		"plain":           "---\nname: x\ndescription: does a thing well\n---\nbody\n",
		"quoted manual":   "---\nname: x\ndescription: does a thing well\ndisable-model-invocation: \"true\"\n---\n",
		"indented manual": "---\nname: x\ndescription: does a thing well\nmeta:\n  disable-model-invocation: true\n---\n",
		"never closed":    "---\nname: x\ndescription: does a thing well\nbody goes on\n",
		"byte-order mark": "\ufeff---\nname: x\ndescription: does a thing well\n---\nbody\n",
		"block scalar":    "---\nname: x\ndescription: |\n  line one\n  line two\n---\n",
		"no frontmatter":  "just text\n",
	} {
		_, chars := scan.SkillListing("x", []byte(doc))
		if got, want := reapTokens("x", doc), bytesToTokens(chars); got != want {
			t.Errorf("%s: reinvest %d tok, reap %d", name, got, want)
		}
	}
}

// A candidate saved with a byte-order mark has the description reap reads, and
// is not dropped as one with none.
func TestCandidates_a_skill_with_a_byte_order_mark_is_offered(t *testing.T) {
	a := fakeApp(t)
	doc := "\ufeff---\nname: release\ndescription: cuts a release: bumps the version, writes the changelog, tags\n---\nbody\n"
	a.skillDoc = func(candidate) string { return doc }
	found := []candidate{{ID: "acme/tools@release", Kind: kindSkill, Name: "release", Slug: "release", Source: "acme/tools", Installs: minInstalls, Hits: map[string]int{"release": 0}}}
	out, refused := a.candidatesFor(kind{Name: "Releases", Task: "cut a release", Queries: []string{"release"}}, found, true)
	if len(out) != 1 || out[0].Desc == "" || len(refused) != 0 {
		t.Errorf("offered %+v, refused %+v", out, refused)
	}
}
