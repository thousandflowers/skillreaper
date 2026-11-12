package term

import "syscall"

// suspendKeyOff has nothing to check: Linux has no delayed suspend key.
func suspendKeyOff(syscall.Termios) bool { return true }

// threadID names the calling thread for interruptThread.
func threadID() int { return syscall.Gettid() }

// interruptThread sends sig to that one thread: Linux would otherwise hand a
// process's signal to its main thread, which is not the one waiting.
func interruptThread(tid int, sig syscall.Signal) error {
	return syscall.Tgkill(syscall.Getpid(), tid, sig)
}
