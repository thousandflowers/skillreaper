package scan

// StripJSONC removes // line comments, /* */ block comments and trailing
// commas from a JSONC document, leaving valid JSON.
//
// It tracks string state, which is the whole difficulty: the naive approach of
// deleting from "//" to end of line cuts the middle out of every URL in the
// file. The first line of a real opencode.jsonc is a "$schema" key whose value
// is an https:// URL, and a naive stripper turns it into an unterminated
// string — reported by encoding/json as "invalid control character", which
// points nowhere near the cause.
//
// Byte offsets are not preserved: comments are dropped rather than blanked, so
// error positions from a later Unmarshal refer to the stripped text.
func StripJSONC(b []byte) []byte {
	out := make([]byte, 0, len(b))
	const (
		normal = iota
		inString
		inLine
		inBlock
	)
	state := normal
	escaped := false

	for i := 0; i < len(b); i++ {
		c := b[i]
		switch state {
		case normal:
			switch {
			case c == '"':
				state = inString
				out = append(out, c)
			case c == '/' && i+1 < len(b) && b[i+1] == '/':
				state = inLine
				i++
			case c == '/' && i+1 < len(b) && b[i+1] == '*':
				state = inBlock
				i++
			default:
				out = append(out, c)
			}
		case inString:
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				state = normal
			}
		case inLine:
			// A line comment ends at the newline, which is kept so line counts
			// in any later error stay close to the original.
			if c == '\n' {
				state = normal
				out = append(out, c)
			}
		case inBlock:
			if c == '*' && i+1 < len(b) && b[i+1] == '/' {
				state = normal
				i++
			}
		}
	}
	return stripTrailingCommas(out)
}

// stripTrailingCommas removes a comma that is followed only by whitespace and
// a closing brace or bracket. Runs after comment removal, since a comma may be
// separated from its closer by a comment that no longer exists.
func stripTrailingCommas(b []byte) []byte {
	out := make([]byte, 0, len(b))
	inString, escaped := false, false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if inString {
			out = append(out, c)
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(b) && (b[j] == ' ' || b[j] == '\t' || b[j] == '\n' || b[j] == '\r') {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}
