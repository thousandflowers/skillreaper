package reinvest

import (
	"math"
	"strings"
	"testing"
)

// The Python-semantics helpers (pyjson, pynum, pystr, casing, htmlunescape,
// nfkc) at their edges. Every expected value written as Python's was checked
// against Python 3.15 (the prototype's version) when the test was written.

// --- pyjson: reading -------------------------------------------------------

func TestLogicCov_parseJSON_refuses_malformed_text_with_the_reason(t *testing.T) {
	cases := []struct{ in, reason string }{
		{`{1: 2}`, "expecting a property name in double quotes"},
		{`{"a" 1}`, "expecting ':'"},
		{`{"a": }`, "expecting value"},
		{`{"a": 1`, "expecting ',' or '}'"},
		{`{"a": 1 "b": 2}`, "expecting ',' or '}'"},
		{`{"a` + "\x01" + `": 1}`, "invalid control character"},
		{`[1`, "expecting ',' or ']'"},
		{`[1 2]`, "expecting ',' or ']'"},
		{`[1, ]`, "expecting value"},
		{`-`, "expecting value"},
		{`-x`, "expecting value"},
		{`"abc`, "unterminated string"},
		{`"abc\`, "unterminated string"},
		{`"\x"`, `invalid \escape`},
		{`"\u12"`, `invalid \uXXXX escape`},
		{`"\uzzzz"`, `invalid \uXXXX escape`},
		{`"\ud800\u12"`, `invalid \uXXXX escape`},
		{`1.`, "extra data"}, // the dot needs a digit: "1" is read, "." is left over
		{`1e`, "extra data"},
		{`1e+`, "extra data"},
		{`nul`, "expecting value"},
	}
	for _, c := range cases {
		v, err := parseJSON(c.in)
		if err == nil {
			t.Errorf("parseJSON(%q) = %v, want an error", c.in, v)
			continue
		}
		if !strings.Contains(err.Error(), c.reason) {
			t.Errorf("parseJSON(%q) error %q, want it to say %q", c.in, err, c.reason)
		}
	}
}

func TestLogicCov_parseJSON_reads_numbers_as_Python_does(t *testing.T) {
	v, err := parseJSON(`[-Infinity, Infinity, 1e999, 1.5E+2, -0, -0.0, 12345678901234567890, 7, 2.5e-3]`)
	if err != nil {
		t.Fatal(err)
	}
	got := v.([]any)
	if f, ok := got[0].(float64); !ok || !math.IsInf(f, -1) {
		t.Errorf("-Infinity read as %#v", got[0])
	}
	if f, ok := got[1].(float64); !ok || !math.IsInf(f, 1) {
		t.Errorf("Infinity read as %#v", got[1])
	}
	if f, ok := got[2].(float64); !ok || !math.IsInf(f, 1) { // Python: inf, not an error
		t.Errorf("1e999 read as %#v", got[2])
	}
	if got[3] != 150.0 {
		t.Errorf("1.5E+2 read as %#v", got[3])
	}
	if got[4] != int64(0) { // -0 is an int in Python
		t.Errorf("-0 read as %#v", got[4])
	}
	if f, ok := got[5].(float64); !ok || f != 0 || !math.Signbit(f) {
		t.Errorf("-0.0 read as %#v", got[5])
	}
	if got[6] != bigInt("12345678901234567890") {
		t.Errorf("a big integer read as %#v", got[6])
	}
	if got[7] != int64(7) || got[8] != 0.0025 {
		t.Errorf("plain numbers read as %#v %#v", got[7], got[8])
	}
}

func TestLogicCov_parseJSONAt_reads_one_value_and_ignores_what_follows(t *testing.T) {
	v, err := parseJSONAt(`xx1.y`, 2, false)
	if err != nil || v != int64(1) {
		t.Errorf("got %#v, %v; want 1 (the dot without a digit is not part of it)", v, err)
	}
}

func TestLogicCov_parseJSON_surrogates(t *testing.T) {
	cases := map[string]string{
		`"😀"`:            "😀",
		`"\ud800\u0041"`: loneSurrogate + "A", // a high half followed by a plain escape: the escape is still read
		`"\udc00"`:       loneSurrogate,
		`"\ud800"`:       loneSurrogate,
		`"\ud800x"`:      loneSurrogate + "x",
		`"\u00e9\/\b"`:   "é/\b",
	}
	for in, want := range cases {
		v, err := parseJSON(in)
		if err != nil || v != want {
			t.Errorf("parseJSON(%s) = %q, %v; want %q", in, v, err, want)
		}
	}
}

func TestLogicCov_parseJSON_caps_nesting(t *testing.T) {
	deep := func(n int) string { return strings.Repeat("[", n) + strings.Repeat("]", n) }
	if _, err := parseJSON(deep(maxJSONDepth)); err != nil {
		t.Errorf("%d levels: %v", maxJSONDepth, err)
	}
	_, err := parseJSON(deep(maxJSONDepth + 1))
	if err == nil || !strings.Contains(err.Error(), "nested too deeply") {
		t.Errorf("%d levels: got %v, want a refusal", maxJSONDepth+1, err)
	}
	obj := strings.Repeat(`{"a": `, maxJSONDepth+1) + "1" + strings.Repeat("}", maxJSONDepth+1)
	if _, err := parseJSON(obj); err == nil {
		t.Error("objects nested too deeply were read")
	}
}

func TestLogicCov_parseJSON_errors_inside_containers_propagate(t *testing.T) {
	for _, in := range []string{`{"a": [1, }`, `{"a": "x` + "\x02" + `"}`, `[[1, ]`, `{"a": {"b": }}`} {
		if v, err := parseJSON(in); err == nil {
			t.Errorf("parseJSON(%q) = %v, want an error", in, v)
		}
	}
}

// --- pyjson: values --------------------------------------------------------

func TestLogicCov_obj_nil_is_an_empty_object(t *testing.T) {
	var o *obj
	if o.len() != 0 || o.keyList() != nil || o.val("x") != nil {
		t.Error("a nil object is not empty")
	}
}

func TestLogicCov_truthy_is_Pythons_bool(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{nil, false}, {false, false}, {true, true}, {int64(0), false}, {int64(-1), true},
		{bigInt("123456789012345678901"), true}, {0.0, false}, {0.5, true}, {math.NaN(), true},
		{"", false}, {"0", true}, {[]any{}, false}, {[]any{nil}, true}, {newObj(), false},
		{objOf("a", nil), true}, {struct{}{}, true},
	}
	for _, c := range cases {
		if got := truthy(c.v); got != c.want {
			t.Errorf("truthy(%#v) = %v, want %v", c.v, got, c.want)
		}
	}
}

func TestLogicCov_jsonEqual_is_Pythons_equality(t *testing.T) {
	cases := []struct {
		a, b any
		want bool
	}{
		{[]any{int64(1), "a"}, []any{1.0, "a"}, true},
		{[]any{int64(1)}, []any{int64(1), int64(2)}, false},
		{[]any{int64(1), "a"}, []any{int64(1), "b"}, false},
		{[]any{}, newObj(), false},
		{objOf("a", int64(1), "b", "x"), objOf("b", "x", "a", 1.0), true}, // order does not count
		{objOf("a", int64(1)), objOf("a", int64(1), "b", nil), false},
		{objOf("a", int64(1)), objOf("b", int64(1)), false},
		{objOf("a", int64(1)), objOf("a", int64(2)), false},
		{objOf("a", nil), []any{nil}, false},
		{int64(3), 3.0, true},
		{3.0, int64(3), true},
		{3.5, int64(3), false},
		{int64(3), 3.5, false},
		{"3", int64(3), false},
		{nil, nil, true},
		// Python: True == 1, False == 0.0, and an int equals a float only exactly.
		{true, int64(1), true},
		{int64(0), false, true},
		{false, 0.0, true},
		{1.0, true, true},
		{true, int64(2), false},
		{true, "True", false},
		{true, true, true},
		{true, false, false},
		{int64(1<<53 + 1), float64(1 << 53), false},
		{float64(1 << 53), int64(1<<53 + 1), false},
		{int64(math.MaxInt64), math.Ldexp(1, 63), false},
		{int64(1), math.Inf(1), false},
		{int64(0), math.NaN(), false},
	}
	for _, c := range cases {
		if got := jsonEqual(c.a, c.b); got != c.want {
			t.Errorf("jsonEqual(%#v, %#v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// --- pyjson: writing -------------------------------------------------------

func TestLogicCov_dumpJSON_writes_what_json_dumps_writes(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{[]any{math.NaN(), math.Inf(1), math.Inf(-1), 1e16, 1e-5, 0.0001, 123.0, 1e22},
			`[NaN, Infinity, -Infinity, 1e+16, 1e-05, 0.0001, 123.0, 1e+22]`},
		{[]string{"a", "é"}, `["a", "\u00e9"]`},
		{[]string{}, `[]`},
		{7, `7`},
		{bigInt("123456789012345678901"), `123456789012345678901`},
		{struct{}{}, `null`}, // not a JSON value: written as null rather than failing the write
		{objOf("k", "😀\x01\"\\\n\r\t\b\f"), `{"k": "\ud83d\ude00\u0001\"\\\n\r\t\b\f"}`},
		{"\x7f", `"\u007f"`}, // Python escapes DEL too: only space to ~ stay as they are
	}
	for _, c := range cases {
		if got := dumpJSON(c.v); got != c.want {
			t.Errorf("dumpJSON(%#v) = %s, want %s", c.v, got, c.want)
		}
	}
	if got := dumpJSONText([]string{"é", "😀"}); got != `["é", "😀"]` {
		t.Errorf("dumpJSONText kept no characters: %s", got)
	}
}

// --- pynum -----------------------------------------------------------------

func TestLogicCov_pyInt_is_int_of_v_or_0(t *testing.T) {
	cases := []struct {
		v      any
		want   int
		wantOK bool
	}{
		{nil, 0, true}, {"", 0, true}, {false, 0, true}, {true, 1, true},
		{int64(42), 42, true}, {int64(-42), -42, true},
		{int64(maxSafeInt) + 1, 0, false}, {int64(-maxSafeInt) - 1, 0, false},
		{3.9, 3, true}, {-3.9, -3, true}, // int() truncates toward zero
		{math.NaN(), 0, false}, {math.Inf(1), 0, false}, {1e12, 0, false},
		{" 7 ", 7, true}, {"-5", -5, true}, {"+5", 5, true}, {"1_000", 1000, true},
		{" ١٢ ", 12, true}, // any script's decimal digits
		{"𝟘𝟙", 1, true},    // mathematical double-struck digits: outside the 16-bit table
		{"_1", 0, false}, {"1_", 0, false}, {"1__0", 0, false}, {" ", 0, false}, {"-", 0, false},
		{"+-5", 0, false}, {"- 5", 0, false}, {"1.5", 0, false}, {"²", 0, false},
		{"99999999999", 0, false},
		{bigInt("123456789012345678901"), 0, false}, {[]any{int64(1)}, 0, false}, {objOf("a", 1), 0, false},
	}
	for _, c := range cases {
		got, ok := pyInt(c.v)
		if got != c.want || ok != c.wantOK {
			t.Errorf("pyInt(%#v) = %d, %v; want %d, %v", c.v, got, ok, c.want, c.wantOK)
		}
	}
}

func TestLogicCov_listIndex_takes_whole_non_negative_numbers(t *testing.T) {
	cases := []struct {
		v      any
		want   int
		wantOK bool
	}{
		{int64(0), 0, true}, {int64(3), 3, true}, {int64(-1), 0, false}, {int64(maxSafeInt) + 1, 0, false},
		{"12", 12, true}, {"٣", 3, true}, {"1_2", 0, false}, {"", 0, false}, {" 1", 0, false}, {"-1", 0, false},
		{true, 0, false}, {3.0, 0, false}, {nil, 0, false},
	}
	for _, c := range cases {
		got, ok := listIndex(c.v)
		if got != c.want || ok != c.wantOK {
			t.Errorf("listIndex(%#v) = %d, %v; want %d, %v", c.v, got, ok, c.want, c.wantOK)
		}
	}
}

func TestLogicCov_groupIndex_takes_ints_and_bools(t *testing.T) {
	cases := []struct {
		v      any
		want   int
		wantOK bool
	}{
		{true, 1, true}, {false, 0, true}, {int64(2), 2, true}, {int64(-1), 0, false},
		{int64(maxSafeInt) + 1, 0, false}, {"1", 0, false}, {1.0, 0, false}, {nil, 0, false},
	}
	for _, c := range cases {
		got, ok := groupIndex(c.v)
		if got != c.want || ok != c.wantOK {
			t.Errorf("groupIndex(%#v) = %d, %v; want %d, %v", c.v, got, ok, c.want, c.wantOK)
		}
	}
}

func TestLogicCov_decimalValue_every_script(t *testing.T) {
	cases := map[rune]int{'7': 7, '٣': 3, '९': 9, '𝟗': 9, '𝟎': 0, '𑁦': 0}
	for r, want := range cases {
		if got, ok := decimalValue(r); !ok || got != want {
			t.Errorf("decimalValue(%q) = %d, %v; want %d", r, got, ok, want)
		}
	}
	for _, r := range []rune{'a', '²', '½', 'Ⅻ'} {
		if _, ok := decimalValue(r); ok {
			t.Errorf("decimalValue(%q) read a digit", r)
		}
	}
}

// --- pystr -----------------------------------------------------------------

func TestLogicCov_firstRunes_and_lastRunes_count_characters(t *testing.T) {
	cases := []struct {
		s           string
		n           int
		first, last string
	}{
		{"héllo", 2, "hé", "lo"},
		{"héllo", 4, "héll", "éllo"},
		{"héllo", 5, "héllo", "héllo"},
		{"héllo", 9, "héllo", "héllo"},
		{"😀😁😂", 1, "😀", "😂"},
		{"", 3, "", ""},
		{"abc", 0, "", ""}, // the last 0 characters (Python's s[-0:] would be all of s: no caller asks for it)
		{"abc", -1, "", ""},
	}
	for _, c := range cases {
		if got := firstRunes(c.s, c.n); got != c.first {
			t.Errorf("firstRunes(%q, %d) = %q, want %q", c.s, c.n, got, c.first)
		}
		if got := lastRunes(c.s, c.n); got != c.last {
			t.Errorf("lastRunes(%q, %d) = %q, want %q", c.s, c.n, got, c.last)
		}
	}
}

func TestLogicCov_decodeUTF8Replace_sizes_each_replacement_as_Python(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{[]byte("\xed\xa0\x80"), "���"}, // a surrogate: 0xed takes only 80-9f after it
		{[]byte("\xed\x9f\xbf"), "퟿"},
		{[]byte("\xf0\x9f\x98"), "�"}, // a cut emoji is one replacement
		{[]byte("\xf0\x80"), "��"},
		{[]byte("\xf4\x90\x80\x80"), "����"}, // past U+10FFFF
		{[]byte("\xf4\x8f\xbf"), "�"},
		{[]byte("\xe0\x80"), "��"},
		{[]byte("\xe1\x80"), "�"},
		{[]byte("a\xffb"), "a�b"},
		{[]byte("\xc2"), "�"},
	}
	for _, c := range cases {
		if got := decodeUTF8Replace(c.in); got != c.want {
			t.Errorf("decodeUTF8Replace(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLogicCov_pyRepr_and_pyStr(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, "None"}, {true, "True"}, {false, "False"}, {5, "5"}, {int64(-5), "-5"},
		{bigInt("123456789012345678901"), "123456789012345678901"},
		{math.Inf(1), "inf"}, {math.Inf(-1), "-inf"}, {math.NaN(), "nan"}, {1e16, "1e+16"}, {0.1, "0.1"},
		{"\r", `'\r'`}, {"\u0378", `'\u0378'`}, {"\U000e0001", `'\U000e0001'`}, {"\x7f", `'\x7f'`},
		{"a'b", `"a'b"`}, {"a'b\"c", `'a\'b"c'`}, {"é😀", "'é😀'"}, {"\\", `'\\'`},
		{[]any{int64(1), "a", nil}, "[1, 'a', None]"},
		{objOf("k", []any{true}), "{'k': [True]}"},
		{struct{}{}, ""},
	}
	for _, c := range cases {
		if got := pyRepr(c.v); got != c.want {
			t.Errorf("pyRepr(%#v) = %s, want %s", c.v, got, c.want)
		}
	}
	if pyStr("plain") != "plain" || pyStr(int64(3)) != "3" {
		t.Error("pyStr does not leave a string as it is")
	}
}

// --- casing ----------------------------------------------------------------

func TestLogicCov_pyLower_picks_the_final_sigma_as_Python(t *testing.T) {
	cases := map[string]string{
		"ΑΣ'":       "ας'", // ignorable after: still the end of the word
		"Α'Σ":       "α'ς", // ignorable before: skipped back to the alpha
		"ΑΣ'Α":      "ασ'α",
		"Σ":         "σ",
		"'Σ":        "'σ",
		"AΣ.":       "aς.",
		"ΑΣ́":       "ας́",
		"ΟΔΟΣ ΟΔΟΣ": "οδος οδος",
		"İ":         "i̇",
	}
	for in, want := range cases {
		if got := pyLower(in); got != want {
			t.Errorf("pyLower(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- htmlunescape and nfkc -------------------------------------------------

func TestLogicCov_htmlUnescape_numeric_references(t *testing.T) {
	cases := map[string]string{
		"&#;":                       "&#;",
		"&#x;":                      "&#x;",
		"a&#99999999999999999999;b": "a�b", // too large to parse: past the last code point
		"&#xd800;":                  "�",
		"&#0;":                      "�",
		"&#128;":                    "€",
		"&#x110000;":                "�",
		"&#65":                      "A",
		"&#X41;":                    "A",
		"cu&#27;rl":                 "curl", // a disallowed control character produces nothing
	}
	for in, want := range cases {
		if got := htmlUnescape(in); got != want {
			t.Errorf("htmlUnescape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLogicCov_nfkc_empty_and_leading_mark(t *testing.T) {
	if got := string(compose(loadNFKC(), nil)); got != "" {
		t.Errorf("compose(nil) = %q", got)
	}
	cases := map[string]string{
		"":                         "",
		"\u0301e":                  "\u0301e", // a mark with no starter joins nothing
		"e\u0301":                  "\u00e9",
		"\uff43\uff55\uff52\uff4c": "curl",
		"\u1100\u1161":             "\uac00",
	}
	for in, want := range cases {
		if got := nfkc(in); got != want {
			t.Errorf("nfkc(%q) = %q, want %q", in, got, want)
		}
	}
}
