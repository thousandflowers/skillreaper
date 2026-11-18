// Package jsonedit changes one value in a JSON document and leaves every other
// byte alone. Decoding and re-encoding a config file sorts its keys, drops its
// trailing newline, escapes & < > and rewrites its indent, so a reversible edit
// of someone's settings would show up as a rewrite of the whole file.
package jsonedit

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ErrNotFound reports a path that names nothing in the document.
var ErrNotFound = errors.New("not found")

// member is one object member or array element, as byte offsets.
type member struct {
	key              string
	start, valS, end int // start is the key's quote; for an array element, start == valS
}

// Get returns the raw value at path.
func Get(doc []byte, path ...string) ([]byte, error) {
	s, e, err := locate(doc, path)
	if err != nil {
		return nil, err
	}
	return doc[s:e], nil
}

// Delete removes the value at path with its separator, and returns the new
// document and the removed value.
func Delete(doc []byte, path ...string) ([]byte, []byte, error) {
	if len(path) == 0 {
		return nil, nil, errors.New("jsonedit: empty path")
	}
	ps, pe, err := locate(doc, path[:len(path)-1])
	if err != nil {
		return nil, nil, err
	}
	ms, i, err := child(doc, ps, path[len(path)-1])
	if err != nil {
		return nil, nil, err
	}
	m := ms[i]
	from, to := 0, 0
	switch {
	case len(ms) == 1: // nothing left: the container becomes empty
		from, to = ps+1, pe-1
	case i == len(ms)-1:
		from, to = ms[i-1].end, m.end
	default:
		from, to = m.start, ms[i+1].start
	}
	return splice(doc, from, to, nil), doc[m.valS:m.end], nil
}

// Set puts value at path, replacing what is there or adding it after the last
// member, creating the objects on the way as needed.
func Set(doc, value []byte, path ...string) ([]byte, error) {
	if len(path) == 0 {
		return nil, errors.New("jsonedit: empty path")
	}
	if s, e, err := locate(doc, path); err == nil {
		return splice(doc, s, e, value), nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// Walk down to the deepest object that exists; the rest is built in value.
	for depth := len(path) - 1; depth >= 0; depth-- {
		ps, pe, err := locate(doc, path[:depth])
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		nested := value
		for i := len(path) - 1; i > depth; i-- {
			k, _ := json.Marshal(path[i])
			nested = []byte("{" + string(k) + ":" + string(nested) + "}")
		}
		k, _ := json.Marshal(path[depth])
		return addMember(doc, ps, pe, k, nested)
	}
	return nil, ErrNotFound
}

// Append adds value as the last element of the array at path, creating the
// array when it is absent.
func Append(doc, value []byte, path ...string) ([]byte, error) {
	s, e, err := locate(doc, path)
	if errors.Is(err, ErrNotFound) {
		return Set(doc, []byte("["+string(value)+"]"), path...)
	}
	if err != nil {
		return nil, err
	}
	if doc[s] != '[' {
		return nil, fmt.Errorf("jsonedit: %v is not an array", path)
	}
	return addMember(doc, s, e, nil, value)
}

// addMember inserts into the container at doc[ps:pe] in the style the document
// already uses. A nil key means an array element.
func addMember(doc []byte, ps, pe int, key, value []byte) ([]byte, error) {
	if doc[ps] != '{' && key != nil {
		return nil, ErrNotFound
	}
	ms, err := members(doc, ps)
	if err != nil {
		return nil, err
	}
	pretty := bytes.IndexByte(doc, '\n') >= 0
	if len(ms) > 0 {
		pretty = bytes.ContainsAny(leadingSpace(doc, ms[0].start), "\n")
	}
	var buf bytes.Buffer
	if !pretty {
		if err := json.Compact(&buf, value); err != nil {
			return nil, err
		}
		member := append(keyPrefix(key, ":"), buf.Bytes()...)
		if len(ms) == 0 {
			return splice(doc, ps+1, pe-1, member), nil
		}
		return splice(doc, ms[len(ms)-1].end, ms[len(ms)-1].end, append([]byte(","), member...)), nil
	}
	indent := lineIndent(doc, ps) + indentUnit(doc)
	if len(ms) > 0 {
		sp := leadingSpace(doc, ms[len(ms)-1].start)
		indent = string(sp[bytes.LastIndexByte(sp, '\n')+1:])
	}
	if err := json.Indent(&buf, value, indent, indentUnit(doc)); err != nil {
		return nil, err
	}
	member := append(keyPrefix(key, ": "), buf.Bytes()...)
	if len(ms) == 0 {
		body := "\n" + indent + string(member) + "\n" + lineIndent(doc, ps)
		return splice(doc, ps+1, pe-1, []byte(body)), nil
	}
	last := ms[len(ms)-1]
	add := "," + string(leadingSpace(doc, last.start)) + string(member)
	return splice(doc, last.end, last.end, []byte(add)), nil
}

func keyPrefix(key []byte, sep string) []byte {
	if key == nil {
		return nil
	}
	return append(append([]byte{}, key...), sep...)
}

func splice(doc []byte, from, to int, with []byte) []byte {
	out := make([]byte, 0, len(doc)-(to-from)+len(with))
	out = append(out, doc[:from]...)
	out = append(out, with...)
	return append(out, doc[to:]...)
}

// leadingSpace is the whitespace that ends at offset i.
func leadingSpace(doc []byte, i int) []byte {
	j := i
	for j > 0 && isSpace(doc[j-1]) {
		j--
	}
	return doc[j:i]
}

// lineIndent is the indentation of the line holding offset i.
func lineIndent(doc []byte, i int) string {
	start := bytes.LastIndexByte(doc[:i], '\n') + 1
	end := start
	for end < len(doc) && (doc[end] == ' ' || doc[end] == '\t') {
		end++
	}
	return string(doc[start:end])
}

// indentUnit is the indent of the document's first member, or two spaces.
func indentUnit(doc []byte) string {
	s := skipSpace(doc, 0)
	if ms, err := members(doc, s); err == nil && len(ms) > 0 {
		sp := leadingSpace(doc, ms[0].start)
		if i := bytes.LastIndexByte(sp, '\n'); i >= 0 && i+1 < len(sp) {
			return string(sp[i+1:])
		}
	}
	return "  "
}

// locate finds the value a path names. An empty path is the whole document.
func locate(doc []byte, path []string) (int, int, error) {
	if !json.Valid(doc) {
		return 0, 0, errors.New("jsonedit: not valid JSON")
	}
	s := skipSpace(doc, 0)
	e := skipValue(doc, s)
	for _, step := range path {
		ms, i, err := child(doc, s, step)
		if err != nil {
			return 0, 0, err
		}
		s, e = ms[i].valS, ms[i].end
	}
	return s, e, nil
}

// child finds one step of a path inside the container at offset s. When an
// object repeats a key the last one counts, as it does in encoding/json.
func child(doc []byte, s int, step string) ([]member, int, error) {
	ms, err := members(doc, s)
	if err != nil {
		return nil, 0, err
	}
	if doc[s] == '[' {
		i, err := strconv.Atoi(step)
		if err != nil || i < 0 || i >= len(ms) {
			return nil, 0, fmt.Errorf("%w: index %s", ErrNotFound, step)
		}
		return ms, i, nil
	}
	for i := len(ms) - 1; i >= 0; i-- {
		if ms[i].key == step {
			return ms, i, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: %s", ErrNotFound, step)
}

// members lists the container at offset s. The document was validated by locate
// or is the validated output of an earlier edit, so offsets cannot run away.
func members(doc []byte, s int) ([]member, error) {
	if s >= len(doc) || (doc[s] != '{' && doc[s] != '[') {
		return nil, ErrNotFound
	}
	isObject := doc[s] == '{'
	var out []member
	i := skipSpace(doc, s+1)
	for doc[i] != '}' && doc[i] != ']' {
		m := member{start: i, valS: i}
		if isObject {
			keyEnd := skipString(doc, i)
			if err := json.Unmarshal(doc[i:keyEnd], &m.key); err != nil {
				return nil, err
			}
			i = skipSpace(doc, keyEnd)
			i = skipSpace(doc, i+1) // past the colon
			m.valS = i
		}
		m.end = skipValue(doc, i)
		out = append(out, m)
		i = skipSpace(doc, m.end)
		if doc[i] == ',' {
			i = skipSpace(doc, i+1)
		}
	}
	return out, nil
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func skipSpace(doc []byte, i int) int {
	for i < len(doc) && isSpace(doc[i]) {
		i++
	}
	return i
}

func skipString(doc []byte, i int) int {
	for i++; doc[i] != '"'; i++ {
		if doc[i] == '\\' {
			i++
		}
	}
	return i + 1
}

// skipValue returns the offset just past the value starting at i.
func skipValue(doc []byte, i int) int {
	switch doc[i] {
	case '"':
		return skipString(doc, i)
	case '{', '[':
		depth := 0
		for {
			switch doc[i] {
			case '"':
				i = skipString(doc, i)
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
	}
	for i < len(doc) && !isSpace(doc[i]) && doc[i] != ',' && doc[i] != '}' && doc[i] != ']' {
		i++
	}
	return i
}
