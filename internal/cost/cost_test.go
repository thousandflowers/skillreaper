package cost

import (
	"math"
	"testing"
)

func TestTokens(t *testing.T) {
	cases := []struct {
		name       string
		chars, want int
	}{
		{"zero", 0, 0},
		{"one char", 1, 1},
		{"exact ratio 37 chars", 37, 10},
		{"round up 38 chars", 38, 11},
		{"ten ratios 370 chars", 370, 100},
		{"large number", 3700, 1000},
		{"negative", -5, 0},
		{"edge 36", 36, 10},
		{"edge 39", 39, 11},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Tokens(c.chars); got != c.want {
				t.Errorf("Tokens(%d) = %d, want %d", c.chars, got, c.want)
			}
		})
	}
}

func TestMoneyPerMonth(t *testing.T) {
	cases := []struct {
		name                   string
		tokens, sessions int
		price, want       float64
	}{
		{"standard", 1000, 60, 3.0, 0.18},
		{"zero tokens", 0, 100, 3.0, 0},
		{"zero sessions", 1000, 0, 3.0, 0},
		{"zero price", 1000, 60, 0, 0},
		{"expensive model", 5000, 20, 5.0, 0.5},
		{"cheap model", 5000, 20, 0.15, 0.015},
		{"single token", 1, 1, 3.0, 0.000003},
		{"large values", 100000, 100, 10.0, 100},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MoneyPerMonth(c.tokens, c.sessions, c.price)
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("MoneyPerMonth(%d,%d,%.2f) = %f, want %f", c.tokens, c.sessions, c.price, got, c.want)
			}
		})
	}
}

func TestLookupPrice(t *testing.T) {
	cases := []struct {
		name    string
		modelID string
		wantOK  bool
	}{
		{"claude sonnet", "claude-sonnet-4-6", true},
		{"claude opus 4.7", "claude-opus-4-7", true},
		{"gpt-4o", "gpt-4o", true},
		{"unknown model", "unknown-model", false},
		{"empty string", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, ok := LookupPrice(c.modelID)
			if ok != c.wantOK {
				t.Errorf("LookupPrice(%q) ok = %v, want %v", c.modelID, ok, c.wantOK)
			}
		})
	}
}

func TestLookupPriceKnownModels(t *testing.T) {
	models := []struct {
		name  string
		id    string
		wantPrice float64
	}{
		{"opus 4.7", "claude-opus-4-7", 5.0},
		{"opus 4.6", "claude-opus-4-6", 5.0},
		{"sonnet 4.6", "claude-sonnet-4-6", 3.0},
		{"haiku 4.5", "claude-haiku-4-5", 1.0},
		{"gpt-4o", "gpt-4o", 2.50},
		{"gpt-4o-mini", "gpt-4o-mini", 0.15},
		{"o3-mini", "o3-mini", 1.10},
		{"claude 3.5 sonnet", "claude-3-5-sonnet", 3.0},
	}
	for _, m := range models {
		t.Run(m.name, func(t *testing.T) {
			p, ok := LookupPrice(m.id)
			if !ok {
				t.Errorf("LookupPrice(%q) not found", m.id)
			}
			if math.Abs(p-m.wantPrice) > 1e-9 {
				t.Errorf("LookupPrice(%q) = %f, want %f", m.id, p, m.wantPrice)
			}
		})
	}
}

func TestDefaultModel(t *testing.T) {
	if DefaultModel == "" {
		t.Error("DefaultModel must not be empty")
	}
	if _, ok := LookupPrice(DefaultModel); !ok {
		t.Errorf("DefaultModel %q is not in ModelPricing", DefaultModel)
	}
}
