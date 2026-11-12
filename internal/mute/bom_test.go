package mute

import (
	"bytes"
	"testing"
)

// A SKILL.md saved with a UTF-8 byte-order mark is read and weighed by reap, so
// mute strips its description too, and gives back every byte, the mark included.
func TestAFileWithAByteOrderMarkIsMutedAndKeepsItsMark(t *testing.T) {
	doc := []byte("\xef\xbb\xbf---\nname: bom\ndescription: words here\n---\nbody\n")

	rest, removed, found := splitKey(doc, false)

	if !found || !bytes.HasPrefix(rest, []byte("\xef\xbb\xbf---\n")) || bytes.Contains(rest, []byte("description")) {
		t.Errorf("found %v, rest %q", found, rest)
	}
	if string(removed) != "description: words here\n" {
		t.Errorf("removed %q", removed)
	}
}
