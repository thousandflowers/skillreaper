package menu

// Ported from the asserts at the bottom of experiments/menu-prototype/currency.py:
// a wrong amount is worse than a foreign one.

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// eur is what a Mac set to euros answers: its currency and its separators.
var eur = systemStyle{code: "EUR", sym: "€", decimal: ",", group: "."}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestCurrencyForStaysInDollarsWithoutAUsableRate(t *testing.T) {
	t.Run("no answer from the system", func(t *testing.T) {
		mustEqual(t, "currency", currencyFor(systemStyle{}, false, map[string]float64{"USD": 2}), USD)
	})

	t.Run("a currency the table does not hold", func(t *testing.T) {
		mustEqual(t, "currency", currencyFor(systemStyle{code: "XXX", sym: "x"}, true, map[string]float64{"USD": 2}), USD)
	})

	t.Run("a known currency is converted, with the system's separators", func(t *testing.T) {
		want := USD
		want.Code, want.Sym, want.Rate, want.Decimal, want.Group = "EUR", "€", 0.5, ",", "."

		mustEqual(t, "currency", currencyFor(eur, true, map[string]float64{"USD": 2, "EUR": 1}), want)
	})

	t.Run("no table at all", func(t *testing.T) {
		mustEqual(t, "code", currencyFor(eur, true, nil).Code, "USD")
	})

	bad := map[string]float64{"zero": 0, "negative": -1, "not a number": math.NaN(), "infinite": math.Inf(1), "minus infinity": math.Inf(-1)}
	for name, rate := range bad {
		t.Run("its own rate is "+name, func(t *testing.T) {
			got := currencyFor(eur, true, map[string]float64{"USD": 2, "EUR": rate})

			mustEqual(t, "code", got.Code, "USD")
			mustEqual(t, "symbol", got.Sym, USD.Sym)
			mustEqual(t, "rate", got.Rate, 1.0)
		})
		t.Run("the dollar's rate is "+name, func(t *testing.T) {
			mustEqual(t, "code", currencyFor(eur, true, map[string]float64{"USD": rate, "EUR": 1}).Code, "USD")
		})
	}

	t.Run("the dollar's rate is missing", func(t *testing.T) {
		mustEqual(t, "code", currencyFor(eur, true, map[string]float64{"EUR": 1}).Code, "USD")
	})

	t.Run("the separators are the system's either way, the digits are not", func(t *testing.T) {
		yen := systemStyle{code: "JPY", sym: "¥", decimal: ",", group: ".", digits: 0, hasDigits: true}

		got := currencyFor(yen, true, map[string]float64{"USD": 2})

		mustEqual(t, "decimal", got.Decimal, ",")
		mustEqual(t, "group", got.Group, ".")
		mustEqual(t, "digits", got.Digits, USD.Digits)
		mustEqual(t, "symbol", got.Sym, USD.Sym)
	})

	t.Run("a converted currency takes the system's digits", func(t *testing.T) {
		yen := systemStyle{code: "JPY", sym: "¥", digits: 0, hasDigits: true}

		got := currencyFor(yen, true, map[string]float64{"USD": 1, "JPY": 150})

		mustEqual(t, "digits", got.Digits, 0)
		mustEqual(t, "rate", got.Rate, 150.0)
	})
}

func TestParseAnswerReadsWhatTheSystemSaid(t *testing.T) {
	t.Run("the older CODE SYMBOL line", func(t *testing.T) {
		got, ok := parseAnswer("EUR €")

		mustEqual(t, "ok", ok, true)
		mustEqual(t, "style", got, systemStyle{code: "EUR", sym: "€"})
	})

	t.Run("a symbol with a space in it", func(t *testing.T) {
		got, ok := parseAnswer("CHF Fr .")

		mustEqual(t, "ok", ok, true)
		mustEqual(t, "style", got, systemStyle{code: "CHF", sym: "Fr ."})
	})

	t.Run("the JSON answer, with a currency of no decimals", func(t *testing.T) {
		got, ok := parseAnswer(`{"code":"JPY","sym":"¥","decimal":".","group":",","digits":0}`)

		mustEqual(t, "ok", ok, true)
		mustEqual(t, "style", got, systemStyle{code: "JPY", sym: "¥", decimal: ".", group: ",", digits: 0, hasDigits: true})
	})

	nonsense := map[string]string{
		"one word":                      "nope",
		"nothing":                       "",
		"a code that is not one":        "euro €",
		"JSON with no symbol":           `{"code":"EUR"}`,
		"JSON whose code is wrong":      `{"code":"EURO","sym":"€"}`,
		"JSON whose symbol is not text": `{"code":"EUR","sym":3}`,
	}
	for name, out := range nonsense {
		t.Run("makes no sense: "+name, func(t *testing.T) {
			_, ok := parseAnswer(out)

			mustEqual(t, "ok", ok, false)
		})
	}

	t.Run("marks that turn the text around are dropped from the symbol", func(t *testing.T) {
		got, ok := parseAnswer(`{"code":"ILS","sym":"‏₪‎"}`)

		mustEqual(t, "ok", ok, true)
		mustEqual(t, "symbol", got.sym, "₪")
	})

	t.Run("a symbol of nothing but such marks is the code", func(t *testing.T) {
		got, _ := parseAnswer(`{"code":"ILS","sym":"‏ "}`)

		mustEqual(t, "symbol", got.sym, "ILS")
	})

	t.Run("a separator longer than one character is not one", func(t *testing.T) {
		got, _ := parseAnswer(`{"code":"EUR","sym":"€","decimal":",,","group":""}`)

		mustEqual(t, "decimal", got.decimal, "")
		mustEqual(t, "group", got.group, "")
	})

	for name, digits := range map[string]string{"too many": "5", "negative": "-1", "a fraction": "1.5", "text": `"2"`} {
		t.Run("digits that are "+name+" are not taken", func(t *testing.T) {
			got, ok := parseAnswer(`{"code":"EUR","sym":"€","digits":` + digits + `}`)

			mustEqual(t, "ok", ok, true)
			mustEqual(t, "has digits", got.hasDigits, false)
		})
	}
}

func table(t *testing.T, date any, perEUR any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"date": date, "per_eur": perEUR})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReadTableTrustsOnlyAFreshTable(t *testing.T) {
	rates := map[string]float64{"USD": 2, "EUR": 1}
	today := day(2026, time.February, 1)

	t.Run("a month old is fresh", func(t *testing.T) {
		got := readTable(table(t, "2026-01-01", rates), today)

		mustEqual(t, "rates read", len(got), 2)
		mustEqual(t, "dollar", got["USD"], 2.0)
	})

	t.Run("years old is stale: every currency is unknown", func(t *testing.T) {
		mustEqual(t, "rates read", len(readTable(table(t, "2019-01-01", rates), today)), 0)
	})

	t.Run("no date is no table", func(t *testing.T) {
		mustEqual(t, "rates read", len(readTable(table(t, nil, rates), today)), 0)
	})

	t.Run("a date that is not one is no table", func(t *testing.T) {
		mustEqual(t, "rates read", len(readTable(table(t, "last week", rates), today)), 0)
	})

	t.Run("ninety days old is still fresh, ninety-one is not", func(t *testing.T) {
		made := "2026-01-01"

		mustEqual(t, "at ninety days", len(readTable(table(t, made, rates), day(2026, time.April, 1))), 2)
		mustEqual(t, "at ninety-one days", len(readTable(table(t, made, rates), day(2026, time.April, 2))), 0)
	})

	t.Run("the ninetieth day is fresh at any hour of it", func(t *testing.T) {
		afternoon := time.Date(2026, time.April, 1, 15, 0, 0, 0, time.UTC)

		got := readTable(table(t, "2026-01-01", rates), afternoon)

		mustEqual(t, "rates read", len(got), 2)
	})

	t.Run("a table that is not JSON is no table", func(t *testing.T) {
		mustEqual(t, "rates read", len(readTable([]byte("not json"), today)), 0)
	})

	t.Run("a table with no rates converts nothing", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]any{"date": "2026-01-01"})

		mustEqual(t, "code", currencyFor(eur, true, readTable(raw, today)).Code, "USD")
	})

	for name, bad := range map[string]any{"text": "1", "nothing": nil} {
		t.Run("a rate that is "+name+" converts nothing", func(t *testing.T) {
			raw := table(t, "2026-01-01", map[string]any{"USD": 2.0, "EUR": bad})

			mustEqual(t, "code", currencyFor(eur, true, readTable(raw, today)).Code, "USD")
		})
	}
}

func TestShippedRatesAreReadable(t *testing.T) {
	var shipped struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal(ratesJSON, &shipped); err != nil {
		t.Fatalf("rates.json: %v", err)
	}
	made, err := time.Parse(isoDate, shipped.Date)
	if err != nil {
		t.Fatalf("rates.json date %q: %v", shipped.Date, err)
	}

	t.Run("on the day they were made they hold the dollar and the euro", func(t *testing.T) {
		got := readTable(ratesJSON, made)

		for _, code := range []string{"USD", "EUR"} {
			if !usableRate(got[code]) {
				t.Errorf("rate of %s = %v", code, got[code])
			}
		}
	})

	t.Run("every rate shipped is usable and every code is a currency code", func(t *testing.T) {
		for code, rate := range readTable(ratesJSON, made) {
			if !currencyCode.MatchString(code) || !usableRate(rate) {
				t.Errorf("%q: %v", code, rate)
			}
		}
	})

	t.Run("a release left running for months stops converting", func(t *testing.T) {
		got := readTable(ratesJSON, made.Add(maxRateAge+24*time.Hour))

		mustEqual(t, "rates read", len(got), 0)
		mustEqual(t, "currency", currencyFor(eur, true, got).Code, "USD")
	})

	t.Run("euros at the shipped rate cost less than a dollar each, or more: never nothing", func(t *testing.T) {
		got := currencyFor(eur, true, readTable(ratesJSON, made))

		mustEqual(t, "code", got.Code, "EUR")
		if !usableRate(got.Rate) {
			t.Errorf("rate = %v", got.Rate)
		}
	})
}

// env is a getenv over a fixed set of variables.
func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestAskedLocaleIsTheOneTheEnvironmentAsksMoneyToFollow(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"nothing set: the system's own", nil, ""},
		{"LC_MONETARY alone", map[string]string{"LC_MONETARY": "it_IT.UTF-8"}, "it_IT"},
		{"LC_ALL wins over LC_MONETARY", map[string]string{"LC_ALL": "fr_FR.UTF-8", "LC_MONETARY": "it_IT.UTF-8"}, "fr_FR"},
		{"the encoding is dropped", map[string]string{"LC_ALL": "en_US.UTF-8"}, "en_US"},
		{"a modifier is dropped", map[string]string{"LC_ALL": "it_IT@euro"}, "it_IT"},
		{"an encoding and a modifier both", map[string]string{"LC_MONETARY": "de_DE.ISO8859-15@euro"}, "de_DE"},
		{"C is the POSIX locale", map[string]string{"LC_ALL": "C"}, "en_US_POSIX"},
		{"C with an encoding too", map[string]string{"LC_ALL": "C.UTF-8"}, "en_US_POSIX"},
		{"POSIX is the POSIX locale", map[string]string{"LC_MONETARY": "POSIX"}, "en_US_POSIX"},
		{"a language alone", map[string]string{"LC_ALL": "de"}, "de"},
		{"a script and a region", map[string]string{"LC_ALL": "zh_Hans_CN"}, "zh_Hans_CN"},
		{"a value that is not a locale is never passed on", map[string]string{"LC_ALL": "$(id)"}, ""},
		{"a value that is not a locale does not fall through to the next variable", map[string]string{"LC_ALL": "not a locale", "LC_MONETARY": "it_IT"}, ""},
		{"LANG is left alone: the terminal fills it in by itself", map[string]string{"LANG": "it_IT.UTF-8"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustEqual(t, "locale", askedLocale(env(c.vars)), c.want)
		})
	}
}

// standIn writes a shell script that stands where osascript would: it notes how
// it was called beside itself, then does what body says.
func standIn(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ask")
	script := "#!/bin/sh\nprintf '%s\\n' \"$#\" > \"$0.count\"\nfor a in \"$@\"; do last=$a; done\nprintf '%s' \"$last\" > \"$0.last\"\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// calledWith is how many arguments the stand-in was given and the last of them;
// ran is false if it was never run.
func calledWith(t *testing.T, script string) (count, last string, ran bool) {
	t.Helper()
	n, err := os.ReadFile(script + ".count")
	if err != nil {
		return "", "", false
	}
	l, _ := os.ReadFile(script + ".last")
	return strings.TrimSpace(string(n)), string(l), true
}

const euroAnswer = `printf '%s' '{"code":"EUR","sym":"€","decimal":",","group":".","digits":2}'`

func TestSystemCurrencyAsksOnlyAMac(t *testing.T) {
	t.Run("off macOS nothing is asked and nothing is run", func(t *testing.T) {
		script := standIn(t, euroAnswer)

		_, ok := systemCurrency(asker{goos: "linux", command: script, getenv: env(nil)})

		_, _, ran := calledWith(t, script)
		mustEqual(t, "answered", ok, false)
		mustEqual(t, "command run", ran, false)
	})

	t.Run("on macOS the answer is read as the system gave it", func(t *testing.T) {
		script := standIn(t, euroAnswer)

		got, ok := systemCurrency(asker{goos: "darwin", command: script, getenv: env(nil)})

		mustEqual(t, "answered", ok, true)
		mustEqual(t, "style", got, systemStyle{code: "EUR", sym: "€", decimal: ",", group: ".", digits: 2, hasDigits: true})
	})

	t.Run("with no locale asked for, the question carries none", func(t *testing.T) {
		script := standIn(t, euroAnswer)

		systemCurrency(asker{goos: "darwin", command: script, getenv: env(nil)})

		count, last, _ := calledWith(t, script)
		mustEqual(t, "arguments", count, "4") // -l JavaScript -e <the question>
		mustEqual(t, "last argument", last, askSystem)
	})

	t.Run("the locale the environment asks for is handed over, cleaned", func(t *testing.T) {
		script := standIn(t, euroAnswer)

		systemCurrency(asker{goos: "darwin", command: script, getenv: env(map[string]string{"LC_MONETARY": "it_IT.UTF-8"})})

		count, last, _ := calledWith(t, script)
		mustEqual(t, "arguments", count, "5")
		mustEqual(t, "last argument", last, "it_IT")
	})

	failures := map[string]string{
		"the command fails":          "exit 1",
		"the command prints rubbish": "printf 'no idea'",
		"the command prints nothing": "true",
		"the answer has no currency": `printf '%s' '{"sym":"€"}'`,
		"the answer fails after it":  euroAnswer + "\nexit 3",
	}
	for name, body := range failures {
		t.Run("no answer when "+name, func(t *testing.T) {
			_, ok := systemCurrency(asker{goos: "darwin", command: standIn(t, body), getenv: env(nil)})

			mustEqual(t, "answered", ok, false)
		})
	}

	t.Run("no answer when the command is not there", func(t *testing.T) {
		_, ok := systemCurrency(asker{goos: "darwin", command: filepath.Join(t.TempDir(), "missing"), getenv: env(nil)})

		mustEqual(t, "answered", ok, false)
	})
}

// ratesMade is the day the shipped table was made.
func ratesMade(t *testing.T) time.Time {
	t.Helper()
	var shipped struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal(ratesJSON, &shipped); err != nil {
		t.Fatal(err)
	}
	made, err := time.Parse(isoDate, shipped.Date)
	if err != nil {
		t.Fatal(err)
	}
	return made
}

func TestCurrencyWithJoinsTheSystemsAnswerAndTheShippedRates(t *testing.T) {
	made := ratesMade(t)
	rates := readTable(ratesJSON, made)
	mac := func(body string) asker { return asker{goos: "darwin", command: standIn(t, body), getenv: env(nil)} }

	t.Run("a Mac set to euros shows euros at the shipped rate", func(t *testing.T) {
		got := currencyWith(mac(euroAnswer), made)

		mustEqual(t, "currency", got, Currency{Code: "EUR", Sym: "€", Rate: rates["EUR"] / rates["USD"], Decimal: ",", Group: ".", Digits: 2})
	})

	t.Run("once the rates are stale it is dollars again, in the system's separators", func(t *testing.T) {
		got := currencyWith(mac(euroAnswer), made.Add(maxRateAge+48*time.Hour))

		mustEqual(t, "currency", got, Currency{Code: "USD", Sym: "US$", Rate: 1, Decimal: ",", Group: ".", Digits: 2})
	})

	t.Run("a system that cannot answer is dollars", func(t *testing.T) {
		mustEqual(t, "currency", currencyWith(mac("exit 1"), made), USD)
	})

	t.Run("off macOS it is dollars", func(t *testing.T) {
		mustEqual(t, "currency", currencyWith(asker{goos: "linux", command: standIn(t, euroAnswer), getenv: env(nil)}, made), USD)
	})

	t.Run("on this machine, whatever it is, the result can be shown", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		got := SystemCurrency(made)

		if got.Rate <= 0 || got.Sym == "" || got.Code == "" || got.Decimal == "" {
			t.Errorf("currency = %+v", got)
		}
	})
}
