package reinvest

import (
	"bytes"
	"strings"
	"testing"
)

// As reap's own --days: a window that is empty or wraps the date arithmetic is
// refused with a word, not read as "no sessions". --mcp means nothing without
// --online, and says so.
func TestArgs_that_cannot_mean_what_they_say_are_refused(t *testing.T) {
	for _, args := range [][]string{{"--days", "0"}, {"--days", "-3"}, {"--days", "36501"}, {"--days", "110000"}, {"--max", "0"}, {"--mcp"}} {
		var out, errOut bytes.Buffer
		if _, code, ok := parseArgs(args, &out, &errOut); ok || code != exitUsage || !strings.Contains(errOut.String(), args[0]) {
			t.Errorf("%v: ok %v, code %d, said %q", args, ok, code, errOut.String())
		}
	}
	var out, errOut bytes.Buffer
	if opts, _, ok := parseArgs([]string{"--online", "--mcp", "--days", "36500", "--max", "1"}, &out, &errOut); !ok || !opts.mcp {
		t.Errorf("good arguments refused: %q", errOut.String())
	}
}

// With --online the queries leave for skills.sh too, and the first line says so.
func TestNotice_names_skills_sh_when_online(t *testing.T) {
	if strings.Contains(noticeFor(options{}), "skills.sh") || !strings.Contains(noticeFor(options{online: true}), "skills.sh") ||
		!strings.Contains(noticeFor(options{online: true, mcp: true}), "MCP registry") {
		t.Errorf("notices: %q / %q", noticeFor(options{}), noticeFor(options{online: true, mcp: true}))
	}
}
