package scan

import "testing"

func TestParseFrontmatter(t *testing.T) {
	src := []byte("---\nname: my-skill\ndescription: \"Does a thing\"\n---\nbody text here")
	name, desc, body := parseFrontmatter(src)
	if name != "my-skill" {
		t.Errorf("name = %q", name)
	}
	if desc != "Does a thing" {
		t.Errorf("description = %q", desc)
	}
	if body != len("body text here") {
		t.Errorf("bodyChars = %d, want %d", body, len("body text here"))
	}
}

func TestParseFrontmatterMissing(t *testing.T) {
	src := []byte("just a plain file\nno frontmatter")
	name, desc, body := parseFrontmatter(src)
	if name != "" || desc != "" {
		t.Errorf("expected empty metadata, got %q / %q", name, desc)
	}
	if body != len(src) {
		t.Errorf("bodyChars = %d, want %d", body, len(src))
	}
}

func TestParseFrontmatterUnterminated(t *testing.T) {
	src := []byte("---\nname: broken\nnever closed")
	name, _, body := parseFrontmatter(src)
	if name != "" {
		t.Errorf("expected empty name for unterminated frontmatter, got %q", name)
	}
	if body != len(src) {
		t.Errorf("bodyChars = %d, want %d", body, len(src))
	}
}

func TestParseFrontmatterNoDescription(t *testing.T) {
	src := []byte("---\nname: bare\n---\n")
	name, desc, _ := parseFrontmatter(src)
	if name != "bare" || desc != "" {
		t.Errorf("got %q / %q", name, desc)
	}
}

func TestParseFrontmatterEmpty(t *testing.T) {
	src := []byte("")
	name, desc, body := parseFrontmatter(src)
	if name != "" || desc != "" || body != 0 {
		t.Errorf("expected all empty, got %q / %q / %d", name, desc, body)
	}
}

func TestParseFrontmatterOnlyFrontmatter(t *testing.T) {
	src := []byte("---\nname: justmeta\n---\n")
	name, _, _ := parseFrontmatter(src)
	if name != "justmeta" {
		t.Errorf("name = %q", name)
	}
}

func TestParseFrontmatterMultiLineDescription(t *testing.T) {
	src := []byte("---\nname: multiline\ndescription: \"line1\\nline2\\nline3\"\n---\nbody")
	name, desc, _ := parseFrontmatter(src)
	if name != "multiline" || desc != "line1\nline2\nline3" {
		t.Errorf("got %q / %q", name, desc)
	}
}

// A single-quoted YAML scalar carries no escapes: the backslash-n is two
// literal characters and has to survive as such.
func TestParseFrontmatterSingleQuotedKeepsBackslash(t *testing.T) {
	src := []byte("---\nname: single\ndescription: 'line1\\nline2'\n---\nbody")
	_, desc, _ := parseFrontmatter(src)
	if desc != `line1\nline2` {
		t.Errorf("desc = %q, want the backslash kept", desc)
	}
}

// An escape the unquoter does not recognise must not lose the value: a
// Windows path in a double-quoted description is the realistic case.
func TestParseFrontmatterUnknownEscapeFallsBack(t *testing.T) {
	src := []byte("---\nname: winpath\ndescription: \"C:\\Users\\me\"\n---\nbody")
	_, desc, _ := parseFrontmatter(src)
	if desc != `C:\Users\me` {
		t.Errorf("desc = %q, want the raw text back", desc)
	}
}
