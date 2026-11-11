package term

import (
	"bufio"
	"os/exec"
	"time"
)

// keyboardScript asks macOS for the keyboard a hundred times a second and prints
// a line only when Shift or s changes. The system answers any process, with no
// permission. It ends when the menu that started it is gone.
const keyboardScript = `ObjC.import('CoreGraphics'); ObjC.import('Foundation'); ObjC.import('unistd');
var out = $.NSFileHandle.fileHandleWithStandardOutput, last = '', n = 0;
while (true) {
  var shift = (($.CGEventSourceFlagsState(0) | $.CGEventSourceFlagsState(1)) & 0x20000) ? '1' : '0';
  var now = shift + ($.CGEventSourceKeyState(0, 1) ? '1' : '0');
  if (now !== last) {
    out.writeData($.NSString.alloc.initWithUTF8String(now + '\n').dataUsingEncoding($.NSUTF8StringEncoding));
    last = now;
  }
  if (++n % 100 === 0 && $.getppid() === 1) break;
  $.usleep(10000);
}`

// startKeyboardWatcher starts the helper. Terminal.app sends Shift+Up, Shift+Down
// and Shift+Return as the plain keys, so only the system can say Shift is down.
// It answers nil over ssh and wherever the helper cannot start.
func startKeyboardWatcher() *keyboardWatcher {
	if remoteSession() {
		return nil
	}
	cmd := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", keyboardScript)
	lines, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	w := &keyboardWatcher{cmd: cmd, alive: true}
	go func() {
		scan := bufio.NewScanner(lines)
		for scan.Scan() {
			w.feed(scan.Text(), time.Now())
		}
		w.ended()
		_ = cmd.Wait()
	}()
	return w
}
