package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thousandflowers/skillreaper/internal/menu"
)

// An item another run pruned after this one read the stack is already out of
// the way: mute skips it, as prune and delete do, instead of failing on it.
func TestMuteOfAnItemPrunedMeanwhileIsNotAFailure(t *testing.T) {
	// Arrange
	home := deleteHome(t)
	dir := filepath.Join(home, ".claude")
	row := menuRow(t, reportRows(t), "skill", "copied")
	if err := os.Rename(filepath.Join(dir, "skills", "copied"), filepath.Join(home, "elsewhere")); err != nil {
		t.Fatal(err)
	}

	// Act
	err := applyOne(t, dir, row, menu.Mute)

	// Assert
	if err != nil {
		t.Errorf("muting what is gone: %v", err)
	}
	if !goneSinceScan(&os.PathError{Op: "lstat", Path: "x", Err: os.ErrNotExist}) || goneSinceScan(os.ErrPermission) {
		t.Error("goneSinceScan reads the wrong errors")
	}
}
