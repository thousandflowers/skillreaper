package reinvest

import (
	"strings"
	"testing"
)

// A key pasted into a session must not reach Claude in any call, nor the memory
// file: grouping sends the first prompts of every session, not only writing.
const pastedKey = "sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZ0123"

func TestRedact_a_key_typed_in_a_session_is_gone_before_anything_keeps_it(t *testing.T) {
	s := addPrompt(session{Skills: map[string]bool{}}, "deploy the site with "+pastedKey+" please, it is urgent", nil)
	if len(s.Prompts) != 1 || strings.Contains(s.Prompts[0], pastedKey) || !strings.Contains(s.Prompts[0], secretRemoved) {
		t.Fatalf("the prompt kept is %q", s.Prompts)
	}
	if body := numbered([]session{s}); strings.Contains(body, pastedKey) {
		t.Errorf("the grouping body carries the key: %q", body)
	}
}

func TestRedact_a_key_in_an_older_memory_file_is_gone_on_reading(t *testing.T) {
	v, err := parseJSON(`{"id": "a", "prompts": ["use ` + pastedKey + ` for the release"]}`)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := rememberedSession(v)
	if !ok || len(s.Prompts) != 1 || strings.Contains(s.Prompts[0], pastedKey) {
		t.Errorf("a remembered session kept %q (ok %v)", s.Prompts, ok)
	}
}
