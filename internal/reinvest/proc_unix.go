//go:build unix

package reinvest

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// ownGroup puts the step in a process group of its own, and makes its timeout
// end the whole group: npx starts node and node starts git, and one of them
// left alive could finish an install after reinvest reported it failed.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

// exitOnSignal ends reinvest for a signal. A variable only so a test can see
// the code instead of the test binary ending.
var exitOnSignal = os.Exit

// runGrouped runs cmd, and if Ctrl-C (or a stop) arrives meanwhile, ends the
// step's group before reinvest ends: in a group of its own the step does not
// get the terminal's signal, and a child left alive could finish an install
// that nobody checks.
func runGrouped(cmd *exec.Cmd) error {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case sig := <-sigs:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		err := <-done
		exitOnSignal(128 + int(sig.(syscall.Signal)))
		return err
	}
}
