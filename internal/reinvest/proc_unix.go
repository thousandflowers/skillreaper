//go:build unix

package reinvest

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the step in a process group of its own, and makes its timeout
// end the whole group: npx starts node and node starts git, and one of them
// left alive could finish an install after reinvest reported it failed.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
