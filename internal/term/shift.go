package term

import (
	"os"
	"os/exec"
	"sync"
	"time"
)

// shiftGrace is how long Shift still counts after it was last seen down: the
// system's state can miss a quick Shift+Enter when asked only as the key arrives.
const shiftGrace = 300 * time.Millisecond

// remoteVars mean the keyboard the system would be asked about is not the one typing.
var remoteVars = []string{"SSH_CONNECTION", "SSH_TTY"}

func remoteSession() bool {
	for _, name := range remoteVars {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}

// keyboardWatcher follows the Shift and s keys through a helper that asks the
// system, where the terminal itself hides them. A nil watcher knows nothing.
type keyboardWatcher struct {
	mu         sync.Mutex
	cmd        *exec.Cmd
	alive      bool
	shift, s   bool
	releasedAt time.Time
}

// feed takes one line of the helper: two digits, Shift then s.
func (w *keyboardWatcher) feed(line string, now time.Time) {
	if len(line) != 2 || (line[0] != '0' && line[0] != '1') || (line[1] != '0' && line[1] != '1') {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.shift && line[0] == '0' {
		w.releasedAt = now
	}
	w.shift, w.s = line[0] == '1', line[1] == '1'
}

func (w *keyboardWatcher) ended() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.alive, w.shift, w.s = false, false, false
}

func (w *keyboardWatcher) shiftHeld(now time.Time) bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.shift || (!w.releasedAt.IsZero() && now.Sub(w.releasedAt) < shiftGrace)
}

func (w *keyboardWatcher) sHeld() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.s
}

func (w *keyboardWatcher) readable() bool {
	if w == nil {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alive
}

func (w *keyboardWatcher) stop() {
	if w == nil || w.cmd == nil || w.cmd.Process == nil {
		return
	}
	_ = w.cmd.Process.Kill()
}
