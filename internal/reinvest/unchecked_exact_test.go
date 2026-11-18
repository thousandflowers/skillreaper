package reinvest

import (
	"path/filepath"
	"testing"
)

// The installed files are what the verifier read, byte for byte: a file cut
// short (the guard after a command dropped), an emptied one, or one gone is not.
func TestUnchecked_a_file_cut_short_emptied_or_gone_is_not_what_was_checked(t *testing.T) {
	tree := map[string]string{
		skillFile:      "---\nname: demo\ndescription: x\n---\nStep 1: clean $TARGET.\nStep 2: TARGET is the build folder, never home.\n",
		"scripts/a.sh": "rm -rf \"$TARGET\"/*\n[ -n \"$TARGET\" ] || exit 1\n",
		"scripts/b.sh": "echo b\n",
		"notes.md":     "kept whole\n",
	}
	c := candidate{Kind: kindSkill, Slug: "demo", Doc: renderTree(tree)}
	home := tempHome(t)
	root := filepath.Join(home, ".claude", "skills", "demo")
	writeFile(t, filepath.Join(root, skillFile), "---\nname: demo\ndescription: x\n---\nStep 1: clean $TARGET.\n")
	writeFile(t, filepath.Join(root, "scripts", "a.sh"), "rm -rf \"$TARGET\"/*\n")
	writeFile(t, filepath.Join(root, "scripts", "b.sh"), "")
	writeFile(t, filepath.Join(root, "notes.md"), "kept whole\n")

	got := uncheckedFiles(c)

	if !sameWords(got, []string{skillFile, "scripts/a.sh", "scripts/b.sh"}) {
		t.Errorf("unchecked %v", got)
	}
	writeFile(t, filepath.Join(root, skillFile), tree[skillFile])
	writeFile(t, filepath.Join(root, "scripts", "a.sh"), tree["scripts/a.sh"])
	writeFile(t, filepath.Join(root, "scripts", "b.sh"), tree["scripts/b.sh"])
	if got := uncheckedFiles(c); len(got) != 0 {
		t.Errorf("the very files checked are unchecked: %v", got)
	}
	if got := uncheckedFiles(candidate{Kind: kindSkill, Slug: "demo", Doc: renderTree(map[string]string{skillFile: tree[skillFile], "gone.md": "x\n"})}); !sameWords(got, []string{"gone.md", "notes.md", "scripts/a.sh", "scripts/b.sh"}) {
		t.Errorf("a file checked and not installed, and files installed and not checked: %v", got)
	}
}
