package reinvest

import "testing"

// Two candidates can share a name (one per source). The swap opens on the one
// it was sized for, not on its namesake.
func TestBuild_a_swap_opens_on_its_own_pick_not_a_namesake(t *testing.T) {
	heavy, light := 100, 60
	zed := candidate{ID: "zed/one@code-review", Kind: kindSkill, Name: "code-review", Gap: "Reviews", Fit: 4, Cost: &heavy}
	alpha := candidate{ID: "alpha/two@code-review", Kind: kindSkill, Name: "code-review", Gap: "Reviews", Fit: 4, Cost: &light}
	pool := []skill{{Name: "old", Tokens: 100}} // what pairs with the light one weighs like the heavy one

	swaps := build([]candidate{alpha}, []candidate{zed, alpha}, pool)

	if len(swaps) != 1 || swaps[0].cand().ID != alpha.ID {
		t.Errorf("the swap opens on %+v, want %s", swaps, alpha.ID)
	}
}
