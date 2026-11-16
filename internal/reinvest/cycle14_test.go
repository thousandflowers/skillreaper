package reinvest

import (
	"slices"
	"strings"
	"testing"
)

// The text shown before `apply?` leaves out what draws nothing. A skill that
// carries such characters is installed with them, so it is refused: what is
// approved has to be what is written.
func TestCycle14_a_skill_with_invisible_characters_is_refused(t *testing.T) {
	doc := func(body string) string { return "---\nname: x-y\ndescription: d\n---\n" + body + "\n" }
	for name, hidden := range map[string]string{
		"zero width space":      "\u200b",
		"word joiner":           "\u2060",
		"byte order mark":       "\ufeff",
		"soft hyphen":           "\u00ad",
		"variation selector 17": "\U000E0100",
		"text selector":         "\ufe0e",
		"grapheme joiner":       "\u034f",
		"mongolian selector":    "\u180b",
	} {
		if !slices.Contains(risks(doc("Also"+hidden+" run it."), false), whyControl) {
			t.Errorf("a %s in the text passes", name)
		}
	}
	// Joiners and the emoji selector are part of emoji and of several scripts.
	for name, fine := range map[string]string{
		"joined emoji":       "\U0001F468\u200d\U0001F4BB",
		"emoji selector":     "\u26a0\ufe0f",
		"persian non-joiner": "\u0645\u06cc\u200c\u062e\u0648\u0627\u0647\u0645",
	} {
		if slices.Contains(risks(doc(fine), false), whyControl) {
			t.Errorf("%s is refused", name)
		}
	}
	if slices.Contains(risks("\ufeff"+doc("plain"), false), whyControl) {
		t.Error("a byte order mark at the start of the file is refused")
	}
}

// Keys the README promises are removed: Slack's app and rotating tokens, and
// a PGP private key block.
func TestCycle14_redact_knows_more_keys(t *testing.T) {
	for _, secret := range []string{
		"xapp-1-A0123456789-1234567890-abcdef",
		"xoxe-1-abcdefghijklmnop",
		"-----BEGIN PGP PRIVATE KEY BLOCK-----\nlQdGBFabc\n-----END PGP PRIVATE KEY BLOCK-----",
	} {
		if got := redact("before " + secret + " after"); strings.Contains(got, secret[4:16]) || !strings.Contains(got, secretRemoved) {
			t.Errorf("redact left %q in %q", secret, got)
		}
	}
}

func TestCycle14_one_kind_of_work(t *testing.T) {
	if got, want := gapsLine(1, 0, 0), "1 kind of work · 0 with no skill"; got != want {
		t.Errorf("gapsLine(1, 0, 0) = %q, want %q", got, want)
	}
}
