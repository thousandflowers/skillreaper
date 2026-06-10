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
