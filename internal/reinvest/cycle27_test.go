package reinvest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A transcript that cannot be opened makes the count low too, and is said
// like a folder that cannot be read: it was in the log only.
func TestCycle27_an_unreadable_transcript_is_said(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	a, _ := pipelineApp(t, verdict{Keep: true, Risk: noRisk, Fit: 5, Reason: "fits", Use: "x"})
	closed := filepath.Join(a.projectsDir, "-tmp-project", "closed.jsonl")
	if err := os.WriteFile(closed, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(closed, 0); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o600) })
	second := filepath.Join(a.projectsDir, "-tmp-project", "closed-too.jsonl")
	if err := os.WriteFile(second, []byte("{}\n"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(second, 0o600) })
	_ = a.run(options{days: defaultDays, max: defaultSwaps})
	if said := termText(a); !strings.Contains(said, "✗") || !strings.Contains(said, "could not be read") || !strings.Contains(said, "closed") || !strings.Contains(said, "and 1 more") {
		t.Errorf("one closed transcript:\n%s", said)
	}
}

// Two kinds of work that would go by one name are told apart, and one with
// neither a name nor a task still has something to go by: the second was
// written, verified, and then never offered, without a word.
func TestCycle27_kinds_of_work_never_share_a_name(t *testing.T) {
	sessions := []session{{ID: "s1", Prompts: []string{"p"}}, {ID: "s2", Prompts: []string{"p"}}, {ID: "s3", Prompts: []string{"p"}}}
	groups := []*obj{
		objOf("task", "Fix the failing tests in module A", sessionIDsKey, []any{"s1"}),
		objOf("task", "Fix the failing tests in module B", sessionIDsKey, []any{"s2"}),
		objOf("name", " ", "task", "", sessionIDsKey, []any{"s3"}),
	}
	kinds := kindsOfWork(groups, sessions)
	if len(kinds) != 3 {
		t.Fatalf("%d kinds", len(kinds))
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		if strings.TrimSpace(k.Name) == "" || seen[k.Name] {
			t.Errorf("names: %q, %q, %q", kinds[0].Name, kinds[1].Name, kinds[2].Name)
			break
		}
		seen[k.Name] = true
	}
}
