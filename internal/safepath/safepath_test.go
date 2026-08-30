package safepath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithinDir(t *testing.T) {
	root, _ := filepath.Abs(t.TempDir())
	sep := string(filepath.Separator)
	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{"itself", root, true},
		{"child", filepath.Join(root, "a"), true},
		{"nested child", filepath.Join(root, "a", "b"), true},
		{"dot-dot remains within", root + sep + "a" + sep + ".." + sep + "b", true},
		{"dot-dot escapes", root + sep + ".." + sep + filepath.Base(root) + "-outside", false},
		{"sibling", filepath.Join(filepath.Dir(root), "other"), false},
		{"parent", filepath.Dir(root), false},
		{"prefix trick", filepath.Dir(root) + "x", false},
	}
	for _, c := range cases {
		if got := WithinDir(root, c.target); got != c.want {
			t.Errorf("%s: WithinDir(%q) = %v, want %v", c.name, c.target, got, c.want)
		}
	}
}

func TestWithinDirRejectsEmptyBoundary(t *testing.T) {
	if WithinDir("", t.TempDir()) {
		t.Error("expected an empty root not to contain an unrelated target")
	}
	// t.Chdir so the empty target resolves from a directory this test chose:
	// WithinDir turns "" into the working directory, and the assertion was
	// silently relying on the test binary never being run from inside the
	// temporary root.
	root := t.TempDir()
	t.Chdir(t.TempDir())
	if WithinDir(root, "") {
		t.Error("expected a root not to contain an empty target resolved from the working directory")
	}
}

func TestWithinDirMalformedRoot(t *testing.T) {
	// A malformed root (here containing a NUL) must never be reported as
	// containing a target, regardless of which guard rejects it. Note this
	// does not specifically prove the filepath.Abs error branch fires — Abs
	// does not fail on a NUL on Unix — so the assertion is only that the
	// result is false, not which branch produced it.
	if WithinDir("\x00root", "/tmp") {
		t.Error("expected WithinDir to be false for a malformed root")
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"plain":       "plain",
		"a:b":         "a-b",
		"a/b":         "a-b",
		"a\\b":        "a-b",
		"plugin:name": "plugin-name",
		"":            "",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeMatchesOldBehavior(t *testing.T) {
	// Every char the old Replacer covered must still map to "-".
	for _, c := range []string{":", "/", "\\"} {
		if got := Sanitize(c); got != "-" {
			t.Errorf("Sanitize(%q) = %q, want -", c, got)
		}
	}
	// Confirm an absolute-looking name collapses to a single segment.
	if s := Sanitize("a/b/c"); filepath.Base(s) != s || s != "a-b-c" {
		t.Errorf("Sanitize(a/b/c) = %q, want a-b-c", s)
	}
	// Sanity: result is always usable as a relative path with no separators.
	if strings.ContainsAny(filepath.Base(Sanitize("x")), `/\`) {
		t.Error("sanitized name still contains a path separator")
	}
}

func TestParentWithinForWriteRejectsSymlinkedParentOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "reaped")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	err := ParentWithinForWrite(root, filepath.Join(link, "muted", "state.json"))
	if err == nil {
		t.Fatal("expected symlinked parent outside root to be rejected")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "muted")); !os.IsNotExist(statErr) {
		t.Fatalf("outside directory should not be created, stat err: %v", statErr)
	}
}

func TestExistingRegularFileWithinRejectsFinalSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real.json")
	if err := os.WriteFile(real, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "state.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	_, err := ExistingRegularFileWithin(root, link)
	if err == nil {
		t.Fatal("expected final symlink to be rejected even when it resolves inside root")
	}
	// Asserting on the message, not merely on rejection: os.Lstat reports a
	// symlink as non-regular too, so the IsRegular guard below shadows the
	// symlink guard. Without this the test still passed with the symlink check
	// deleted, pinning that the path is refused but not that it is refused for
	// being a symlink.
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected the symlink guard to reject it, got %v", err)
	}
}

func TestRejectFinalSymlinkAllowsAMissingPath(t *testing.T) {
	root := t.TempDir()
	if err := RejectFinalSymlink(filepath.Join(root, "not-created-yet.json")); err != nil {
		t.Fatalf("a path that does not exist yet must be allowed: %v", err)
	}
}

func TestAtomicWriteFileWithinRejectsFinalSymlinkOutOfRoot(t *testing.T) {
	// The companion of the symlinked-parent case: here every directory on the
	// way is honest and only the final name is a symlink pointing out of the
	// tree. ParentWithinForWrite cannot see it, so RejectFinalSymlink is the
	// only guard standing, and nothing exercised it.
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escaped.json")
	if err := os.WriteFile(outside, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "state.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := AtomicWriteFileWithin(root, link, []byte("overwritten"), 0o600); err == nil {
		t.Fatal("expected a write to a final symlink pointing out of root to be rejected")
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("write escaped root: outside file is now %q", got)
	}
}

func TestReadRegularFileWithinRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "state.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if _, err := ReadRegularFileWithin(root, link, 1024); err == nil {
		t.Fatal("expected a read through a symlink outside root to be rejected")
	}
}

func TestAtomicWriteFileWithinRejectsSymlinkedParentOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "state")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	outsideTarget := filepath.Join(outside, "config.json")
	err := AtomicWriteFileWithin(root, filepath.Join(link, "config.json"), []byte("escaped"), 0o600)
	if err == nil {
		t.Fatal("expected atomic write through a symlinked parent to be rejected")
	}
	if _, statErr := os.Stat(outsideTarget); !os.IsNotExist(statErr) {
		t.Fatalf("write escaped root: stat outside target: %v", statErr)
	}
}

func TestReadRegularFileWithinEnforcesByteCap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadRegularFileWithin(root, path, 4); err == nil {
		t.Fatal("expected an oversized regular file to be rejected")
	}
	got, err := ReadRegularFileWithin(root, path, 5)
	if err != nil {
		t.Fatalf("read at exact byte cap: %v", err)
	}
	if string(got) != "12345" {
		t.Fatalf("read at exact byte cap = %q, want %q", got, "12345")
	}
}

func TestRegularFileHelpersRejectDirectory(t *testing.T) {
	root := t.TempDir()
	if _, err := ExistingRegularFileWithin(root, root); err == nil {
		t.Fatal("expected ExistingRegularFileWithin to reject a directory")
	}
	if _, err := ReadRegularFileWithin(root, root, 1024); err == nil {
		t.Fatal("expected ReadRegularFileWithin to reject a directory")
	}
}

// Issue #36. Sanitize promises a segment that cannot escape its directory, but
// "." and ".." are single filenames that traverse and survive the replacer
// untouched. Neither caller can be made to escape today; the contract is still
// false, and the next caller is the one that pays for it.
func TestSanitizeNeutralizesTraversalSegments(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"..", "--"},
		{".", "-"},
		{"../../etc/passwd", "..-..-etc-passwd"}, // inert once separators go
		{"a/../../b", "a-..-..-b"},               // likewise
		{"..foo", "..foo"},                       // not a traversal segment
		{"normal-name", "normal-name"},
	} {
		if got := Sanitize(tc.in); got != tc.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
