package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestTheMarkdownFloorLineCountsInTheRightNumber(t *testing.T) {
	for n, want := range map[int]string{1: "1 unused item (MCP servers, hooks) carries", 2: "2 unused items (MCP servers, hooks) carry"} {
		var b bytes.Buffer
		RenderMarkdown(&b, &Report{DeadUnknownWeight: n})
		if !strings.Contains(b.String(), want) {
			t.Errorf("%d: %s", n, b.String())
		}
	}
}
