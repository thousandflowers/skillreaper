package menu

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Currency is how amounts are written: the symbol and separators of the system,
// at a fixed rate from US dollars.
type Currency struct {
	Code, Sym      string
	Rate           float64
	Decimal, Group string
	Digits         int
}

// USD is the fallback. Its symbol must read as foreign where $ is local.
var USD = Currency{Code: "USD", Sym: "US$", Rate: 1, Decimal: ".", Group: ",", Digits: 2}

// roundsUp: at one decimal in the unit below, this already prints as 1000,0.
const roundsUp = 0.99995

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}

// number is a figure with the separators of the system's currency format.
func (m *Menu) number(v float64, digits int) string {
	s := strconv.FormatFloat(v, 'f', digits, 64)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteString(m.cur.Group)
		}
		b.WriteRune(c)
	}
	if hasFrac {
		b.WriteString(m.cur.Decimal)
		b.WriteString(frac)
	}
	return sign + b.String()
}

// amount is an amount of money: the symbol is never touched, only the number is
// formatted.
func (m *Menu) amount(v float64) string { return m.cur.Sym + m.number(v, m.cur.Digits) }

// moneyFigure is one rule for every small amount, the big font's too: nothing is
// its symbol and 0, a fraction of a cent says less than a cent.
func (m *Menu) moneyFigure(v float64) string {
	smallest := math.Pow(10, -float64(m.cur.Digits))
	switch {
	case v <= 0:
		return m.cur.Sym + "0"
	case v < smallest:
		return "<" + m.amount(smallest)
	}
	return m.amount(v)
}

// money prices tokens. With no price known a figure here would be invented.
func (m *Menu) money(tok float64) string {
	if m.price == 0 {
		return ""
	}
	return m.moneyFigure(tok * m.price * m.cur.Rate)
}

// worth is how money is worded. The figure is a floor; a flat plan's bill does
// not change, so there it is only a value.
func (m *Menu) worth(text string) string {
	if m.subscriber {
		return "worth " + text + " at API prices"
	}
	return "at least " + text
}

// bigMoneyWords is the same wording around a figure in the big font: above it,
// beside it.
func (m *Menu) bigMoneyWords(words string) (string, string) {
	if m.subscriber {
		return "worth", "at API prices, " + words
	}
	return "at least", words
}

func (m *Menu) moneyColumn() string {
	if m.subscriber {
		return "API PRICE/MONTH"
	}
	return "AT LEAST/MONTH"
}

func (m *Menu) tokens(tok int) string { return m.number(float64(tok), 0) }

// tokMoney is tokens and their money. priced is the same tokens weighed by the
// rows' own sessions.
func (m *Menu) tokMoney(tok int, priced float64) string {
	return strings.TrimRight("~"+m.tokens(tok)+" tok "+m.money(priced), " ")
}

// short is a compact figure, rounded before the unit is chosen so a value just
// under a million reads 1,0M.
func (m *Menu) short(v float64) string {
	if v >= 1e15 {
		return strings.Replace(fmt.Sprintf("%.1e", v), ".", m.cur.Decimal, 1)
	}
	for _, u := range []struct {
		limit float64
		unit  string
	}{{1e12, "T"}, {1e9, "G"}, {1e6, "M"}, {1e3, "k"}} {
		if v >= u.limit*roundsUp {
			return m.number(v/u.limit, 1) + u.unit
		}
	}
	return itoa(int(v))
}

func (m *Menu) shortCount(n int) string {
	if n < 1000 {
		return itoa(n)
	}
	return m.short(float64(n))
}

// sessionsOf is how many sessions a month load this row: its own platform's,
// when the report knows them per platform.
func (m *Menu) sessionsOf(r *Row) int {
	if m.platMonth == nil {
		return m.perMonth
	}
	return m.platMonth[r.raw.Platform]
}

// reach is a row's tokens in the units money() prices: 1 unless its platform has
// more or fewer sessions than the average.
func (m *Menu) reach(r *Row) float64 {
	if m.platMonth == nil || m.price == 0 || m.tokenSessions == 0 {
		return 1
	}
	return float64(m.sessionsOf(r)) * m.rep.MoneyPerMonth / m.tokenSessions / m.price
}

func padLeft(s string, width int) string {
	if n := width - cells(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}
