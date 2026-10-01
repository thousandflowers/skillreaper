package report

import (
	"bytes"
	"strings"
	"testing"
)

// An MCP server's weight is measured from transcripts, not from a description,
// and an unknown one must say why it is unknown.
func TestRenderWhyMCPWeightSource(t *testing.T) {
	for name, c := range map[string]struct {
		e    Explanation
		want string
	}{
		"measured": {Explanation{Name: "srv", Category: "mcp", TokenWeight: 18}, "~18 tok  (tool names + instructions, last session that loaded it)"},
		"unknown":  {Explanation{Name: "srv", Category: "mcp"}, "? (never seen loading in a session, so never measured)"},
		"skill":    {Explanation{Name: "s", Category: "skill", TokenWeight: 5, DescChars: 18}, "~5 tok  (description: 18 chars)"},
	} {
		var b bytes.Buffer
		RenderWhy(&b, c.e, false)
		if !strings.Contains(b.String(), c.want) {
			t.Errorf("%s: want %q in\n%s", name, c.want, b.String())
		}
	}
}
