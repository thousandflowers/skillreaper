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
