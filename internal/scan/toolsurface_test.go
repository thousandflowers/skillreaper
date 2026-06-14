package scan

import "testing"

func TestToolSurface(t *testing.T) {
	cases := []struct {
		name string
		md   string
		keys []string
		want int
	}{
		{"no key = unrestricted", "---\nname: x\ndescription: d\n---\nbody", []string{"allowed-tools"}, ToolSurfaceAll},
		{"star = unrestricted", "---\nname: x\nallowed-tools: \"*\"\n---\n", []string{"allowed-tools"}, ToolSurfaceAll},
		{"empty value = unrestricted", "---\nname: x\nallowed-tools:\n---\n", []string{"allowed-tools"}, ToolSurfaceAll},
		{"one tool", "---\nname: x\nallowed-tools: Read\n---\n", []string{"allowed-tools"}, 1},
		{"three tools", "---\nname: x\nallowed-tools: Read, Edit, Bash\n---\n", []string{"allowed-tools"}, 3},
		{"agent tools key", "---\nname: x\ntools: Read, Grep\n---\n", []string{"tools", "allowed-tools"}, 2},
		{"no frontmatter = unrestricted", "just a body\n", []string{"allowed-tools"}, ToolSurfaceAll},
	}
	for _, c := range cases {
		if got := toolSurface([]byte(c.md), c.keys...); got != c.want {
			t.Errorf("%s: toolSurface = %d, want %d", c.name, got, c.want)
		}
	}
}
