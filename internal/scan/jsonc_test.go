package scan

import (
	"encoding/json"
	"testing"
)

func TestStripJSONC(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// The case that motivated a string-aware stripper. A real
			// opencode.jsonc opens with a "$schema" whose value is an https
			// URL; deleting from "//" to end of line cuts the string open, and
			// encoding/json then reports "invalid control character", which
			// points nowhere near the cause.
			name: "a URL is not a comment",
			in:   `{"$schema": "https://opencode.ai/config.json"}`,
			want: `{"$schema": "https://opencode.ai/config.json"}`,
		},
		{
			// The indentation before "//" is emitted while still outside the
			// comment, so it survives. Harmless in JSON, and preserving it
			// keeps the stripper a single pass.
			name: "line comment",
			in:   "{\n  // why this key exists\n  \"a\": 1\n}",
			want: "{\n  \n  \"a\": 1\n}",
		},
		{
			name: "block comment",
			in:   `{"a": /* inline */ 1}`,
			want: `{"a":  1}`,
		},
		{
			name: "trailing comma before brace",
			in:   `{"a": 1,}`,
			want: `{"a": 1}`,
		},
		{
			name: "trailing comma before bracket",
			in:   `{"a": [1, 2,]}`,
			want: `{"a": [1, 2]}`,
		},
		{
			name: "comment markers inside a string survive",
			in:   `{"a": "/* not a comment */ // nor this"}`,
			want: `{"a": "/* not a comment */ // nor this"}`,
		},
		{
			// An escaped quote must not end the string, or everything after it
			// is treated as code and the next "//" eats real content.
			name: "escaped quote does not end the string",
			in:   `{"a": "he said \" // still inside", "b": 2}`,
			want: `{"a": "he said \" // still inside", "b": 2}`,
		},
		{
			name: "already plain JSON is unchanged",
			in:   `{"a":1,"b":[2,3]}`,
			want: `{"a":1,"b":[2,3]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(StripJSONC([]byte(tt.in)))
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
			// Whatever comes out must be parseable, which is the only reason
			// this function exists.
			var v any
			if err := json.Unmarshal([]byte(got), &v); err != nil {
				t.Errorf("output is not valid JSON: %v", err)
			}
		})
	}
}

func TestStripJSONCHandlesAWholeDocument(t *testing.T) {
	in := `{
  // opencode config
  "$schema": "https://opencode.ai/config.json",
  /* the plugin list
     spans lines */
  "plugin": ["oh-my-opencode@4.19.4"],
  "model": "big-pickle", // trailing comment
}`
	var top map[string]json.RawMessage
	if err := json.Unmarshal(StripJSONC([]byte(in)), &top); err != nil {
		t.Fatalf("did not parse: %v", err)
	}
	for _, k := range []string{"$schema", "plugin", "model"} {
		if _, ok := top[k]; !ok {
			t.Errorf("key %q was lost", k)
		}
	}
}
