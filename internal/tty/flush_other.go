//go:build !darwin && !linux

package tty

import "os"

// flushInput has no dependency-free form here; typed-ahead input stays.
func flushInput(*os.File) {}
