//go:build !unix

package reinvest

import "os/exec"

// ownGroup has no dependency-free form here: a timeout ends the step's own
// process, as exec.CommandContext does.
func ownGroup(*exec.Cmd) {}

// runGrouped is cmd.Run: the step shares the console, and its signal, with reap.
func runGrouped(cmd *exec.Cmd) error { return cmd.Run() }
