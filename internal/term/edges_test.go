package term

import (
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestDecodeCutShort(t *testing.T) {
	for name, input := range map[string]string{
		"Esc O and nothing after":            "\x1bO",
		"an X10 click missing its last byte": "\x1b[M !",
	} {
		if keys, _ := keysFrom(input); len(keys) != 0 {
			t.Errorf("%s gave %v, want no key", name, keys)
		}
	}
	if keys, _ := keysFrom("\x1b[M !!q"); !slices.Equal(keys, []Key{KeyMouse, 'q'}) {
		t.Errorf("a whole X10 click then q gave %v", keys)
	}
}

func TestFrameWideCharacters(t *testing.T) {
	f := newFrame(1, 10)
	f.put(0, 0, "界x", Style{})
	if got, want := f.line(0, 0), "界x\x1b[K"; got != want {
		t.Errorf("a wide character drawn = %q, want %q: its second cell is not drawn again", got, want)
	}
	f.put(0, 0, "a", Style{}) // over the first half: the second half goes too
	if got := f.text(0); !strings.HasPrefix(got, "a x") {
		t.Errorf("after covering half a wide character the row reads %q, want %q", got, "a x")
	}
}

func TestSGRReverse(t *testing.T) {
	if got, want := sgr(Style{Reverse: true}, 0), "\x1b[0;7m"; got != want {
		t.Errorf("reverse = %q, want %q", got, want)
	}
}

func TestFakeSKey(t *testing.T) {
	if (&Fake{}).SHeld() || !(&Fake{SKey: true}).SHeld() {
		t.Error("the fake does not report the s key it was given")
	}
}

func TestRemoteSession(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	if remoteSession() {
		t.Error("a local session reads as remote")
	}
	t.Setenv("SSH_TTY", "/dev/ttys009")
	if !remoteSession() {
		t.Error("SSH_TTY alone does not read as remote")
	}
}

func TestKeyboardWatcherStopKillsTheHelper(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	(&keyboardWatcher{cmd: cmd}).stop()
	err := cmd.Wait()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Errorf("the helper ended with %v, want killed", err)
	}
	(&keyboardWatcher{cmd: exec.Command("true")}).stop() // never started: nothing to kill
}
