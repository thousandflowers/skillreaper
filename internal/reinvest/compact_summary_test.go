package reinvest

import "testing"

// Claude Code writes a compaction summary as a user entry. The model wrote it,
// not the person: it is no prompt of theirs, and goes to no grouping.
func TestSessions_a_compaction_summary_is_not_a_prompt(t *testing.T) {
	for _, flag := range []string{"isCompactSummary", "isVisibleInTranscriptOnly"} {
		entry := objOf("type", "user", flag, true, "message", objOf("content", "This session is being continued from a previous conversation that ran out of context. Summary: ..."))
		if text, ok := userText(entry); ok {
			t.Errorf("%s: read as the person's prompt %q", flag, text)
		}
	}
}
