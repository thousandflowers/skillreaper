package menu

import (
	"context"
	_ "embed" // the fixed exchange-rate table
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

// ratesJSON is the fixed table of ECB reference rates. Nothing is fetched: reap
// sends nothing over the network, so a rate is only as fresh as the release.
//
//go:embed rates.json
var ratesJSON []byte

// askSystem asks macOS for the currency and number style set on this Mac, or
// those of the locale it is handed.
const askSystem = `function run(argv) { ObjC.import("Foundation");
var l = argv.length ? $.NSLocale.localeWithLocaleIdentifier(argv[0]) : $.NSLocale.currentLocale;
var f = $.NSNumberFormatter.alloc.init; f.numberStyle = $.NSNumberFormatterCurrencyStyle; f.locale = l;
return JSON.stringify({code: l.currencyCode.js, sym: l.currencySymbol.js, decimal: l.decimalSeparator.js, group: l.groupingSeparator.js, digits: Number(f.maximumFractionDigits)}) }`

// moneyLocaleVars are where POSIX lets a user say how money is written, the
// first one set winning. LANG is left alone: Terminal fills it in by itself.
var moneyLocaleVars = []string{"LC_ALL", "LC_MONETARY"}

var localeID = regexp.MustCompile(`^[A-Za-z]{2,3}(_[A-Za-z0-9]{2,8}){0,3}$`)

// askedLocale is the locale the environment asks money to follow, or "" for the
// system's own.
func askedLocale(getenv func(string) string) string {
	for _, name := range moneyLocaleVars {
		v := getenv(name)
		if v == "" {
			continue
		}
		id, _, _ := strings.Cut(v, ".")
		id, _, _ = strings.Cut(id, "@")
		switch {
		case id == "C" || id == "POSIX":
			return "en_US_POSIX"
		case localeID.MatchString(id):
			return id
		}
		return ""
	}
	return ""
}

// asker is where the system is asked, and on which system: the tests stand in
// for both, so every branch runs on any machine.
type asker struct {
	goos    string
	command string // never whatever comes first on PATH
	getenv  func(string) string
}

var systemAsker = asker{goos: runtime.GOOS, command: "/usr/bin/osascript", getenv: os.Getenv}

const (
	askTimeout  = 10 * time.Second
	maxRateAge  = 90 * 24 * time.Hour
	maxDigits   = 4
	isoDate     = "2006-01-02"
	baseForRate = "USD" // prices are in US dollars
)

var (
	currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
	bidiMarks    = regexp.MustCompile("[‎‏؜‪-‮⁦-⁩]")
)

// systemStyle is what the system said about money: its currency, and as much of
// the number style as made sense.
type systemStyle struct {
	code, sym      string
	decimal, group string
	digits         int
	hasDigits      bool
}

func oneChar(v any) string {
	if s, ok := v.(string); ok && utf8.RuneCountInString(s) == 1 {
		return s
	}
	return ""
}

// parseAnswer reads the system's JSON, or the older "CODE SYMBOL" line. ok is
// false if it makes no sense.
func parseAnswer(out string) (systemStyle, bool) {
	var ans map[string]any
	if err := json.Unmarshal([]byte(out), &ans); err != nil {
		parts := strings.Fields(out)
		if len(parts) < 2 {
			return systemStyle{}, false
		}
		ans = map[string]any{"code": parts[0], "sym": strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), parts[0]))}
	}
	code, _ := ans["code"].(string)
	sym, isText := ans["sym"].(string)
	if !currencyCode.MatchString(code) || !isText {
		return systemStyle{}, false
	}
	st := systemStyle{code: code, sym: strings.TrimSpace(bidiMarks.ReplaceAllString(sym, "")), decimal: oneChar(ans["decimal"]), group: oneChar(ans["group"])}
	if st.sym == "" {
		st.sym = code
	}
	if d, ok := ans["digits"].(float64); ok && d == math.Trunc(d) && d >= 0 && d <= maxDigits {
		st.digits, st.hasDigits = int(d), true
	}
	return st, true
}

// systemCurrency is the system's currency and number style. ok is false off
// macOS or if it cannot answer.
func systemCurrency(a asker) (systemStyle, bool) {
	if a.goos != "darwin" {
		return systemStyle{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
	defer cancel()
	args := []string{"-l", "JavaScript", "-e", askSystem}
	if id := askedLocale(a.getenv); id != "" {
		args = append(args, id)
	}
	out, err := exec.CommandContext(ctx, a.command, args...).Output()
	if err != nil {
		return systemStyle{}, false
	}
	return parseAnswer(string(out))
}

func usableRate(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 }

// readTable is the fixed rates, or nothing: an unreadable or stale table means
// every currency is unknown. A wrong amount is worse than a foreign one.
func readTable(raw []byte, today time.Time) map[string]float64 {
	var found struct {
		Date   string             `json:"date"`
		PerEUR map[string]float64 `json:"per_eur"`
	}
	if err := json.Unmarshal(raw, &found); err != nil {
		return nil
	}
	made, err := time.Parse(isoDate, found.Date)
	if err != nil {
		return nil
	}
	// Whole calendar days, the reader's: the ninetieth day is fresh until it ends.
	y, mo, d := today.Date()
	if time.Date(y, mo, d, 0, 0, 0, 0, time.UTC).Sub(made) > maxRateAge {
		return nil
	}
	return found.PerEUR
}

// currencyFor turns the system's answer into how amounts are written. A currency
// with no usable rate stays in dollars; the separators are the system's either way.
func currencyFor(found systemStyle, ok bool, perEUR map[string]float64) Currency {
	c := USD
	if !ok {
		return c
	}
	if found.decimal != "" {
		c.Decimal = found.decimal
	}
	if found.group != "" {
		c.Group = found.group
	}
	if !usableRate(perEUR[found.code]) || !usableRate(perEUR[baseForRate]) {
		return c
	}
	if found.hasDigits {
		c.Digits = found.digits
	}
	c.Code, c.Sym, c.Rate = found.code, found.sym, perEUR[found.code]/perEUR[baseForRate]
	return c
}

// SystemCurrency is the currency amounts are shown in: the one set on this Mac,
// at the fixed rate in rates.json. No network.
func SystemCurrency(now time.Time) Currency {
	return currencyWith(systemAsker, now)
}

func currencyWith(a asker, now time.Time) Currency {
	found, ok := systemCurrency(a)
	return currencyFor(found, ok, readTable(ratesJSON, now))
}
