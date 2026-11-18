package term

import "syscall"

// suspendKeyOff says whether Ctrl-Y no longer suspends.
func suspendKeyOff(tio syscall.Termios) bool { return tio.Cc[syscall.VDSUSP] == vdisable }

// threadID names the calling thread for interruptThread; macOS needs none.
func threadID() int { return 0 }

// interruptThread sends sig where a wait in progress on that thread will see
// it. macOS hands a process's signal to a thread blocked in a wait.
func interruptThread(_ int, sig syscall.Signal) error { return syscall.Kill(syscall.Getpid(), sig) }
