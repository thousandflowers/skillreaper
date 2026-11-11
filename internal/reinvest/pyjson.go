package reinvest

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// JSON the way the prototype's json module read and wrote it. encoding/json
// cannot stand in: it forgets the order of keys (a cache key hashes them in
// order), accepts a key given twice (a verdict must not), turns 5 and 5.0 into
// the same number (a fit must be an integer) and spaces its output differently
// (the verifier is shown this text, and its verdict is cached by it).
//
// A value is nil, bool, int64, bigInt, float64, string, []any or *obj.

// bigInt is an integer too large for int64, kept as written.
type bigInt string

// obj is a JSON object that remembers the order its keys came in.
type obj struct {
	keys []string
	vals map[string]any
}

func newObj() *obj { return &obj{vals: map[string]any{}} }

// objOf builds an object from alternating keys and values.
func objOf(pairs ...any) *obj {
	o := newObj()
	for i := 0; i+1 < len(pairs); i += 2 {
		o.set(pairs[i].(string), pairs[i+1])
	}
	return o
}

func (o *obj) len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

func (o *obj) keyList() []string {
	if o == nil {
		return nil
	}
	return o.keys
}

func (o *obj) get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.vals[key]
	return v, ok
}

// val is dict.get(key): nil when the key is missing.
func (o *obj) val(key string) any {
	v, _ := o.get(key)
	return v
}

// set keeps a key's first position and its last value, as a Python dict does.
func (o *obj) set(key string, v any) {
	if _, seen := o.vals[key]; !seen {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// with returns a copy holding one more (or one changed) key.
func (o *obj) with(key string, v any) *obj {
	c := o.without()
	c.set(key, v)
	return c
}

// without returns a copy lacking the given keys.
func (o *obj) without(drop ...string) *obj {
	c := newObj()
	for _, k := range o.keyList() {
		if !containsString(drop, k) {
			c.set(k, o.vals[k])
		}
	}
	return c
}

func (o *obj) str(key string) (string, bool) {
	s, ok := o.val(key).(string)
	return s, ok
}

func (o *obj) object(key string) *obj {
	child, _ := o.val(key).(*obj)
	return child
}

func (o *obj) list(key string) []any {
	items, _ := o.val(key).([]any)
	return items
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// truthy is Python's bool(v) for a JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case bigInt:
		return true
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case *obj:
		return x.len() > 0
	}
	return true
}

// jsonEqual is == between two JSON values: objects compare without order,
// and 1 equals 1.0 as in Python.
func jsonEqual(a, b any) bool {
	switch x := a.(type) {
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !jsonEqual(x[i], y[i]) {
				return false
			}
		}
		return true
	case *obj:
		y, ok := b.(*obj)
		if !ok || x.len() != y.len() {
			return false
		}
		for _, k := range x.keyList() {
			other, has := y.get(k)
			if !has || !jsonEqual(x.vals[k], other) {
				return false
			}
		}
		return true
	case int64:
		if f, ok := b.(float64); ok {
			return float64(x) == f
		}
	case float64:
		if n, ok := b.(int64); ok {
			return x == float64(n)
		}
	}
	return a == b
}

// ---------------------------------------------------------------------------
// Reading
// ---------------------------------------------------------------------------

const maxJSONDepth = 1000 // the prototype's parser gave up on deeper nesting too

var errDuplicateKey = errors.New("duplicate key")

type jsonReader struct {
	src   string
	pos   int
	noDup bool
	depth int
}

func (r *jsonReader) fail(what string) error {
	return fmt.Errorf("json: %s at offset %d", what, r.pos)
}

// parseJSON is json.loads: the whole text must be one value.
func parseJSON(text string) (any, error) { return parseJSONStrict(text, false) }

// parseJSONStrict is json.loads that can also refuse an object naming a key twice.
func parseJSONStrict(text string, noDup bool) (any, error) {
	r := &jsonReader{src: text, noDup: noDup}
	r.skipSpace()
	v, err := r.value()
	if err != nil {
		return nil, err
	}
	r.skipSpace()
	if r.pos != len(r.src) {
		return nil, r.fail("extra data")
	}
	return v, nil
}

// parseJSONAt is JSONDecoder.raw_decode: one value starting exactly at pos,
// whatever follows it.
func parseJSONAt(text string, pos int, noDup bool) (any, error) {
	r := &jsonReader{src: text, pos: pos, noDup: noDup}
	return r.value()
}

func (r *jsonReader) skipSpace() {
	for r.pos < len(r.src) {
		switch r.src[r.pos] {
		case ' ', '\t', '\n', '\r':
			r.pos++
		default:
			return
		}
	}
}

func (r *jsonReader) value() (any, error) {
	if r.pos >= len(r.src) {
		return nil, r.fail("expecting value")
	}
	switch c := r.src[r.pos]; {
	case c == '"':
		r.pos++
		return r.stringBody()
	case c == '{':
		return r.nested(r.object)
	case c == '[':
		return r.nested(r.array)
	case c == '-' || (c >= '0' && c <= '9'):
		if strings.HasPrefix(r.src[r.pos:], "-Infinity") {
			r.pos += len("-Infinity")
			return math.Inf(-1), nil
		}
		return r.number()
	}
	for _, lit := range jsonLiterals {
		if strings.HasPrefix(r.src[r.pos:], lit.word) {
			r.pos += len(lit.word)
			return lit.value, nil
		}
	}
	return nil, r.fail("expecting value")
}

// jsonLiterals are the bare words json.loads accepts, NaN and Infinity included.
var jsonLiterals = []struct {
	word  string
	value any
}{{"null", nil}, {"true", true}, {"false", false}, {"NaN", math.NaN()}, {"Infinity", math.Inf(1)}}

func (r *jsonReader) nested(read func() (any, error)) (any, error) {
	if r.depth >= maxJSONDepth {
		return nil, r.fail("nested too deeply")
	}
	r.depth++
	defer func() { r.depth-- }()
	r.pos++
	return read()
}

func (r *jsonReader) object() (any, error) {
	o := newObj()
	r.skipSpace()
	if r.pos < len(r.src) && r.src[r.pos] == '}' {
		r.pos++
		return o, nil
	}
	for {
		r.skipSpace()
		if r.pos >= len(r.src) || r.src[r.pos] != '"' {
			return nil, r.fail("expecting a property name in double quotes")
		}
		r.pos++
		key, err := r.stringBody()
		if err != nil {
			return nil, err
		}
		r.skipSpace()
		if r.pos >= len(r.src) || r.src[r.pos] != ':' {
			return nil, r.fail("expecting ':'")
		}
		r.pos++
		r.skipSpace()
		v, err := r.value()
		if err != nil {
			return nil, err
		}
		if _, seen := o.vals[key]; seen && r.noDup {
			return nil, errDuplicateKey
		}
		o.set(key, v)
		r.skipSpace()
		if r.pos >= len(r.src) {
			return nil, r.fail("expecting ',' or '}'")
		}
		switch r.src[r.pos] {
		case ',':
			r.pos++
		case '}':
			r.pos++
			return o, nil
		default:
			return nil, r.fail("expecting ',' or '}'")
		}
	}
}

func (r *jsonReader) array() (any, error) {
	items := []any{}
	r.skipSpace()
	if r.pos < len(r.src) && r.src[r.pos] == ']' {
		r.pos++
		return items, nil
	}
	for {
		r.skipSpace()
		v, err := r.value()
		if err != nil {
			return nil, err
		}
		items = append(items, v)
		r.skipSpace()
		if r.pos >= len(r.src) {
			return nil, r.fail("expecting ',' or ']'")
		}
		switch r.src[r.pos] {
		case ',':
			r.pos++
		case ']':
			r.pos++
			return items, nil
		default:
			return nil, r.fail("expecting ',' or ']'")
		}
	}
}

func (r *jsonReader) digits() int {
	start := r.pos
	for r.pos < len(r.src) && r.src[r.pos] >= '0' && r.src[r.pos] <= '9' {
		r.pos++
	}
	return r.pos - start
}

// number reads -?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][-+]?[0-9]+)?, the longest that fits.
func (r *jsonReader) number() (any, error) {
	start := r.pos
	if r.src[r.pos] == '-' {
		r.pos++
	}
	if r.pos < len(r.src) && r.src[r.pos] == '0' {
		r.pos++
	} else if r.digits() == 0 {
		r.pos = start
		return nil, r.fail("expecting value")
	}
	isFloat := false
	if mark := r.pos; r.pos < len(r.src) && r.src[r.pos] == '.' {
		r.pos++
		if r.digits() == 0 {
			r.pos = mark
		} else {
			isFloat = true
		}
	}
	if mark := r.pos; r.pos < len(r.src) && (r.src[r.pos] == 'e' || r.src[r.pos] == 'E') {
		r.pos++
		if r.pos < len(r.src) && (r.src[r.pos] == '+' || r.src[r.pos] == '-') {
			r.pos++
		}
		if r.digits() == 0 {
			r.pos = mark
		} else {
			isFloat = true
		}
	}
	text := r.src[start:r.pos]
	if isFloat {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, r.fail("bad number")
		}
		return f, nil
	}
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return n, nil
	}
	return bigInt(text), nil
}

var jsonEscapes = map[byte]byte{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}

const loneSurrogate = "?" // what a half surrogate pair became once the prototype encoded the text

// stringBody reads a string whose opening quote is already consumed.
func (r *jsonReader) stringBody() (string, error) {
	var b strings.Builder
	for {
		if r.pos >= len(r.src) {
			return "", r.fail("unterminated string")
		}
		c := r.src[r.pos]
		switch {
		case c == '"':
			r.pos++
			return b.String(), nil
		case c < 0x20:
			return "", r.fail("invalid control character")
		case c != '\\':
			end := r.pos + 1
			for end < len(r.src) && r.src[end] != '"' && r.src[end] != '\\' && r.src[end] >= 0x20 {
				end++
			}
			b.WriteString(r.src[r.pos:end])
			r.pos = end
			continue
		}
		r.pos++
		if r.pos >= len(r.src) {
			return "", r.fail("unterminated string")
		}
		esc := r.src[r.pos]
		r.pos++
		if plain, ok := jsonEscapes[esc]; ok {
			b.WriteByte(plain)
			continue
		}
		if esc != 'u' {
			return "", r.fail("invalid \\escape")
		}
		if err := r.unicodeEscape(&b); err != nil {
			return "", err
		}
	}
}

func (r *jsonReader) hex4() (rune, bool) {
	if r.pos+4 > len(r.src) {
		return 0, false
	}
	n, err := strconv.ParseUint(r.src[r.pos:r.pos+4], 16, 32)
	if err != nil {
		return 0, false
	}
	return rune(n), true
}

func (r *jsonReader) unicodeEscape(b *strings.Builder) error {
	hi, ok := r.hex4()
	if !ok {
		return r.fail("invalid \\uXXXX escape")
	}
	r.pos += 4
	if hi < 0xd800 || hi > 0xdfff {
		b.WriteRune(hi)
		return nil
	}
	if hi <= 0xdbff && strings.HasPrefix(r.src[r.pos:], `\u`) {
		mark := r.pos
		r.pos += 2
		if lo, ok := r.hex4(); ok && lo >= 0xdc00 && lo <= 0xdfff {
			r.pos += 4
			b.WriteRune(0x10000 + (hi-0xd800)<<10 + (lo - 0xdc00))
			return nil
		}
		r.pos = mark
	}
	b.WriteString(loneSurrogate)
	return nil
}

// ---------------------------------------------------------------------------
// Writing
// ---------------------------------------------------------------------------

// dumpJSON is json.dumps(v): ", " and ": " between items, and every character
// outside ASCII escaped.
func dumpJSON(v any) string { return dumpJSONWith(v, true) }

// dumpJSONText is json.dumps(v, ensure_ascii=False).
func dumpJSONText(v any) string { return dumpJSONWith(v, false) }

func dumpJSONWith(v any, ascii bool) string {
	var b strings.Builder
	writeJSON(&b, v, ascii)
	return b.String()
}

func writeJSON(b *strings.Builder, v any, ascii bool) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case bigInt:
		b.WriteString(string(x))
	case float64:
		b.WriteString(jsonFloat(x))
	case string:
		writeJSONString(b, x, ascii)
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		writeJSON(b, items, ascii)
	case []any:
		b.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				b.WriteString(", ")
			}
			writeJSON(b, item, ascii)
		}
		b.WriteByte(']')
	case *obj:
		b.WriteByte('{')
		for i, k := range x.keyList() {
			if i > 0 {
				b.WriteString(", ")
			}
			writeJSONString(b, k, ascii)
			b.WriteString(": ")
			writeJSON(b, x.vals[k], ascii)
		}
		b.WriteByte('}')
	default:
		b.WriteString("null")
	}
}

func jsonFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	}
	return pyFloat(f)
}

var jsonShortEscapes = map[rune]string{'"': `\"`, '\\': `\\`, '\n': `\n`, '\r': `\r`, '\t': `\t`, '\b': `\b`, '\f': `\f`}

func writeJSONString(b *strings.Builder, s string, ascii bool) {
	b.WriteByte('"')
	for _, r := range s {
		short, isShort := jsonShortEscapes[r]
		switch {
		case isShort:
			b.WriteString(short)
		case r < 0x20 || (ascii && r > 0x7e && r < 0x10000):
			b.WriteString(`\u` + hexPad(r, 4))
		case ascii && r >= 0x10000:
			r -= 0x10000
			b.WriteString(`\u` + hexPad(0xd800+(r>>10), 4) + `\u` + hexPad(0xdc00+(r&0x3ff), 4))
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}
