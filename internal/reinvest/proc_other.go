//go:build !unix

package reinvest

import "os/exec"

// ownGroup has no dependency-free form here: a timeout ends the step's own
// process, as exec.CommandContext does.
func ownGroup(*exec.Cmd) {}
