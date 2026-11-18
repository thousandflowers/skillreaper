package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/menu"
)

// A server whose name holds a character the screen must not get (here a
// right-to-left override) is shown cleaned, and still acted on by its real name.
func TestAServerWithAnUnprintableNameIsActedOnByItsRealName(t *testing.T) {
	for _, verb := range []string{menu.Prune, menu.Delete} {
		t.Run(verb, func(t *testing.T) {
			// Arrange
			home := deleteHome(t)
			dir := filepath.Join(home, ".claude")
			cfg := filepath.Join(home, ".claude.json")
			body := `{"mcpServers":{"odd` + "\u202e" + `name":{"command":"x"},"other":{"command":"y"}}}`
			if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			row := menuRow(t, reportRows(t), "mcp", "odd?name")

			// Act
			err := applyOne(t, dir, row, verb)

			// Assert
			if err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, cfg); strings.Contains(got, "odd") || !strings.Contains(got, "other") {
				t.Errorf("after %s the config is %s", verb, got)
			}
		})
	}
}
