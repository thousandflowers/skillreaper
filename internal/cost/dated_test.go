package cost

import (
	"math"
	"testing"
)

// Claude Code records some models with their release date in the id. The date
// names a snapshot of the same model, at the same price.
func TestADatedModelIdHasItsModelsPrice(t *testing.T) {
	for _, id := range []string{"claude-haiku-4-5-20251001", "claude-sonnet-4-5-20250929"} {
		undated := id[:len(id)-len("-20251001")]
		want, ok := LookupPrice(undated)
		if !ok {
			t.Fatalf("no price for %s", undated)
		}
		if got, ok := LookupPrice(id); !ok || got != want {
			t.Errorf("%s = %v, %v; want %v", id, got, ok, want)
		}
	}
	if _, ok := LookupPrice("claude-made-up-20251001"); ok {
		t.Error("a dated id of an unknown model got a price")
	}
	haiku, _ := LookupPrice("claude-haiku-4-5")
	opus, _ := LookupPrice("claude-opus-4-6")
	got, ok := Blended(map[string]int{"claude-haiku-4-5-20251001": 90, "claude-opus-4-6[1m]": 10})
	if want := (90*haiku + 10*opus) / 100; !ok || math.Abs(got-want) > 1e-9 {
		t.Errorf("Blended = %v, %v; want %v", got, ok, want)
	}
}
