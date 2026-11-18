package jsonedit

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEditsLeaveOtherBytesAlone(t *testing.T) {
	doc := "{\n    \"z\": \"a&b<c>\",\n    \"mcpServers\": {\n        \"one\": {\"command\": \"x\"},\n        \"two\": {\"command\": \"y\"}\n    },\n    \"a\": 1\n}\n"
	cases := []struct {
		name string
		path []string
		want string
	}{
		{"first", []string{"mcpServers", "one"}, "{\n    \"z\": \"a&b<c>\",\n    \"mcpServers\": {\n        \"two\": {\"command\": \"y\"}\n    },\n    \"a\": 1\n}\n"},
		{"last", []string{"mcpServers", "two"}, "{\n    \"z\": \"a&b<c>\",\n    \"mcpServers\": {\n        \"one\": {\"command\": \"x\"}\n    },\n    \"a\": 1\n}\n"},
		{"lastTop", []string{"a"}, "{\n    \"z\": \"a&b<c>\",\n    \"mcpServers\": {\n        \"one\": {\"command\": \"x\"},\n        \"two\": {\"command\": \"y\"}\n    }\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := Delete([]byte(doc), c.path...)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("got\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

func TestSetThenDeleteIsByteIdentical(t *testing.T) {
	docs := []string{
		"{\n  \"a\": 1\n}\n",
		"{\n\t\"a\": 1\n}",
		`{"a":1}`,
		"{}",
		"{}\n",
		"{\n  \"hooks\": {\n    \"Other\": []\n  }\n}\n",
	}
	for _, doc := range docs {
		set, err := Set([]byte(doc), []byte(`{"command":"a && b"}`), "n")
		if err != nil || !json.Valid(set) {
			t.Fatalf("Set(%q) = %q, %v", doc, set, err)
		}
		if got, err := Get(set, "n"); err != nil || !json.Valid(got) || !bytes.Contains(got, []byte("a && b")) {
			t.Fatalf("Get after Set(%q): %q, %v", doc, got, err)
		}
		back, _, err := Delete(set, "n")
		if err != nil {
			t.Fatal(err)
		}
		if string(back) != doc {
			t.Errorf("round trip of %q gave %q", doc, back)
		}
	}
}

func TestSetReplacesAndAppendGrowsArrays(t *testing.T) {
	out, err := Set([]byte(`{"a":{"b":1},"c":2}`), []byte("9"), "a", "b")
	if err != nil || string(out) != `{"a":{"b":9},"c":2}` {
		t.Fatalf("Set = %s, %v", out, err)
	}
	out, err = Append([]byte("{\n  \"l\": [\n    1\n  ]\n}\n"), []byte(`{"k":"x"}`), "l")
	if err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"l\": [\n    1,\n    {\n      \"k\": \"x\"\n    }\n  ]\n}\n"; string(out) != want {
		t.Errorf("Append = %q", out)
	}
	back, _, err := Delete(out, "l", "1")
	if err != nil || string(back) != "{\n  \"l\": [\n    1\n  ]\n}\n" {
		t.Errorf("Delete element = %q, %v", back, err)
	}
	out, err = Append([]byte(`{}`), []byte(`1`), "l", "m")
	if err != nil || string(out) != `{"l":{"m":[1]}}` {
		t.Errorf("Append creating parents = %s, %v", out, err)
	}
}

func TestStringsWithBracketsDoNotConfuseTheScanner(t *testing.T) {
	doc := `{"a":"}]\"{","b":[{"c":"]"}],"d":true,"e":null}`
	for _, key := range []string{"a", "b", "d", "e"} {
		if _, _, err := Delete([]byte(doc), key); err != nil {
			t.Errorf("Delete %s: %v", key, err)
		}
	}
	out, _, err := Delete([]byte(doc), "b")
	if err != nil || string(out) != `{"a":"}]\"{","d":true,"e":null}` {
		t.Errorf("Delete b = %s, %v", out, err)
	}
}

func TestMissingAndInvalid(t *testing.T) {
	if _, _, err := Delete([]byte(`{"a":1}`), "b"); err == nil {
		t.Error("deleting a missing key must fail")
	}
	if _, _, err := Delete([]byte("// c\n{}"), "b"); err == nil {
		t.Error("a document with comments must be refused")
	}
}
