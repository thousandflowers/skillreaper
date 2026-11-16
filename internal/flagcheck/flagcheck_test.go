package flagcheck

import (
	"flag"
	"io"
	"strings"
	"testing"
)

func TestDecimal(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Int("days", 30, "")
	fs.Float64("price", 0, "")
	fs.String("name", "", "")
	fs.Bool("yes", false, "")
	for _, ok := range [][]string{
		nil, {"--days", "7"}, {"-days=0"}, {"--days=+12"}, {"--days", "-3"}, {"--name", "010"}, {"--price", "010"},
		{"--days", "many"},      // not a number at all: the flag package says so itself
		{"--", "--days", "010"}, // after --, a name is no flag
		{"--days"}, {"mute", "010"}, {"--nope=010"},
	} {
		if err := Decimal(fs, ok); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for _, bad := range [][]string{
		{"--days", "010"}, {"-days=0x10"}, {"--days=1_000"}, {"prune", "--yes", "--days", "0b11"}, {"--days", "00"},
	} {
		err := Decimal(fs, bad)
		if err == nil || !strings.Contains(err.Error(), "-days") || !strings.Contains(err.Error(), "base ten") {
			t.Errorf("%v: %v", bad, err)
		}
	}
}
