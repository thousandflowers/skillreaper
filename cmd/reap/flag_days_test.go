package main

import (
	"strconv"
	"strings"
	"testing"
)

// A day count so large that the date arithmetic wraps would answer with a
// wrong window and no word about it: past a century it is refused.
func TestHugeDayCountsAreRefused(t *testing.T) {
	for _, flag := range []string{"--days", "--grace-days"} {
		for _, n := range []string{"9223372036854775807", strconv.Itoa(maxDays + 1)} {
			code, out := reapRun(flag, n, "--json")
			if code != 1 || !strings.Contains(out, flag+" must be at most 36500, got ") {
				t.Errorf("%s %s: exit %d\n%s", flag, n, code, out)
			}
		}
	}
}

// A price must be a number JSON can hold: Inf passed the check and then broke
// --json and snapshot.
func TestAnEndlessPriceIsRefused(t *testing.T) {
	for _, p := range []string{"Inf", "1e308", "NaN"} {
		if code, out := reapRun("--price="+p, "--json"); code != 1 || !strings.Contains(out, "--price") {
			t.Errorf("--price=%s: exit %d\n%s", p, code, out)
		}
	}
}

// "010" is ten to whoever types it, and the flag package reads it as octal 8:
// the report said "last 8 days". A number not written in base ten is refused.
func TestIntegerFlagsAreBaseTen(t *testing.T) {
	deleteHome(t)
	for _, args := range [][]string{{"--days", "010"}, {"--days=0x10"}, {"gap", "--min-sessions", "1_0"}} {
		code, out := reapRun(args...)
		if code != 2 || !strings.Contains(out, "base ten") {
			t.Errorf("%v: %d %s", args, code, firstLine(out))
		}
	}
	if code, out := reapRun("--days", "10", "--json"); code != 0 {
		t.Errorf("--days 10: %d %s", code, firstLine(out))
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
