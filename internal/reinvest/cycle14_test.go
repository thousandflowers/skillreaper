package reinvest

import (
	"errors"
	"path/filepath"
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

// The way back from a mute is printed to be pasted. With the folder named only
// in this run's environment, the command says it: without, reap unmute looks in
// the usual folder and finds nothing muted.
func TestCycle15_the_undo_of_a_mute_names_the_folder(t *testing.T) {
	dir := filepath.Join(tempHome(t), "claude dir")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	a := fakeApp(t)
	stepsRecorded(a, func([]string) error { return nil })
	s := writtenSwap()
	s.Mute = []skill{{Name: "first", Path: "/s/first/SKILL.md"}}

	log := strings.Join(a.applyOne(s), "\n")

	if want := "(CLAUDE_CONFIG_DIR='" + dir + "' reap unmute /s/first/SKILL.md undoes)"; !strings.Contains(log, want) {
		t.Errorf("the log lacks %q:\n%s", want, log)
	}
}

// A package's metadata has to name the repository itself, not one whose name
// only begins the same way.
func TestCycle15_a_longer_repository_name_is_not_the_repository(t *testing.T) {
	a := fakeApp(t)
	said := ""
	a.httpJSON = func(string, retryPolicy) (any, error) { return js(t, said), nil }
	server := netCovServerObj(t, `{"name": "io.github.acme/tool", "repository": {"url": "https://github.com/acme/tool"},
		"packages": [{"registryType": "npm", "identifier": "acme-tool", "version": "1.0.0"}]}`)
	for meta, want := range map[string]bool{
		`{"repository": "https://github.com/acme/tool-evil"}`:                                             false,
		`{"repository": "https://github.com/acme/tool.git"}`:                                              true,
		`{"homepage": "https://github.com/acme/tool-evil", "repository": "https://github.com/acme/tool"}`: true,
		`{"repository": "https://github.com/acme/tool/tree/main"}`:                                        true,
	} {
		said = meta
		if got := a.packageMatchesRepo(server); got != want {
			t.Errorf("metadata %s: matched %v, want %v", meta, got, want)
		}
	}
}

// A mute that gives no answer in time is cut off, perhaps after it had done
// its work. It is undone like the ones before it; when even that fails, the
// line says nobody knows, not "nothing changed".
func TestCycle15_a_mute_cut_off_for_time_is_undone_too(t *testing.T) {
	late := []string{reapName, "--yes", "mute", "/s/second/SKILL.md"}
	undo := []string{reapName, "unmute", "/s/second/SKILL.md"}
	for name, c := range map[string]struct {
		undoErr error
		want    string
	}{
		"it had muted, and is unmuted": {nil, "  ↺  rolled back: nothing changed"},
		"it cannot be told":            {errors.New("not muted"), "  ?  second: its mute was cut short (reap unmute lists what is muted)"},
	} {
		t.Run(name, func(t *testing.T) {
			tempHome(t)
			a := fakeApp(t)
			calls := stepsRecorded(a, func(cmd []string) error {
				switch {
				case sameWords(cmd, late):
					return noAnswer(120)
				case sameWords(cmd, undo):
					return c.undoErr
				}
				return nil
			})
			s := writtenSwap()
			s.Mute = []skill{{Name: "first", Path: "/s/first/SKILL.md"}, {Name: "second", Path: "/s/second/SKILL.md"}}

			log := a.applyOne(s)

			if !containsCmd(*calls, undo...) {
				t.Errorf("the mute that was cut off was not undone: %v", *calls)
			}
			if n := len(log); n < 2 || log[n-2] != "  ✗  reap mute second: no answer in 120 s" || log[n-1] != c.want {
				t.Errorf("got %q", log)
			}
		})
	}
}
