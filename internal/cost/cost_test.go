package cost

import (
	"math"
	"testing"
)

func TestTokens(t *testing.T) {
	cases := []struct {
		chars, want int
	}{
		{0, 0},
		{1, 1},
		{37, 10},
		{38, 11},
		{370, 100},
	}
	for _, c := range cases {
		if got := Tokens(c.chars); got != c.want {
			t.Errorf("Tokens(%d) = %d, want %d", c.chars, got, c.want)
		}
	}
}

func TestMoneyPerMonth(t *testing.T) {
	// 1000 dead tokens/session x 60 sessions/month x $3/MTok = $0.18
	got := MoneyPerMonth(1000, 60, 3.0)
	if math.Abs(got-0.18) > 1e-9 {
		t.Errorf("MoneyPerMonth = %f, want 0.18", got)
	}
}

func TestMoneyPerMonthZero(t *testing.T) {
	if got := MoneyPerMonth(0, 100, 3.0); got != 0 {
		t.Errorf("MoneyPerMonth with zero tokens = %f, want 0", got)
	}
}
