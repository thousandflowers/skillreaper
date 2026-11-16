package scan

import (
	"encoding/json"
	"strings"
	"testing"
)

// The arguments are joined into one line. One with a space in it must stay one
// word there, or the secret after a flag is masked up to its first space only.
func TestMCPDisplayKeepsAnArgumentWithASpaceWhole(t *testing.T) {
	c := mcpServerConfig{Command: json.RawMessage(`"node"`), Args: []string{"s.js", "--password", "my pass word", "--token=se cret"}}

	got := c.display()

	for _, gone := range []string{"pass word", "cret"} {
		if strings.Contains(got, gone) {
			t.Errorf("display() = %s, which still shows %q", got, gone)
		}
	}
	if !strings.HasPrefix(got, "node s.js --password ") {
		t.Errorf("display() = %s", got)
	}
}
