package reinvest

import "testing"

// The picker's line adds up what will be applied: a candidate accepted in two
// swaps is installed once, with the first swap's mutes.
func TestTotals_count_a_candidate_accepted_twice_once(t *testing.T) {
	cost := 100
	c := candidate{Kind: kindSkill, Name: "release", Cost: &cost}
	twice := []swap{{Cands: []candidate{c}, OK: true, Out: 100}, {Cands: []candidate{c}, OK: true, Out: 100}}
	if got, want := totals(twice), "1 accepted · −100 muted · net +0 tok/session"; got != want {
		t.Errorf("totals = %q, want %q", got, want)
	}
}
