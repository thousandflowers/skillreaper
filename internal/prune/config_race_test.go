package prune

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// claudeWritesMeanwhile makes Claude Code rewrite the config between reap's
// read and reap's write, as a live session does all the time.
func claudeWritesMeanwhile(t *testing.T, cfg, theirs string) {
	t.Helper()
	old := beforeConfigWrite
	t.Cleanup(func() { beforeConfigWrite = old })
	beforeConfigWrite = func() { mustWrite(t, cfg, theirs) }
}

const (
	oursConfig   = `{"mcpServers": {"gone": {"command": "x"}, "keep": {"command": "y"}}}`
	theirsConfig = `{"mcpServers": {"gone": {"command": "x"}, "keep": {"command": "y"}}, "numStartups": 42}`
)

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAConfigChangedUnderAnEditIsLeftAsItsWriterLeftIt(t *testing.T) {
	t.Run("remove", func(t *testing.T) {
		claudeDir := t.TempDir()
		cfg := filepath.Join(claudeDir, "claude.json")
		mustWrite(t, cfg, oursConfig)
		claudeWritesMeanwhile(t, cfg, theirsConfig)

		_, err := RemoveMCP(claudeDir, cfg, "", "gone")

		if !errors.Is(err, ErrConfigChanged) {
			t.Fatalf("RemoveMCP = %v, want ErrConfigChanged", err)
		}
		if got := readString(t, cfg); got != theirsConfig {
			t.Errorf("the config was overwritten: %s", got)
		}
		if entries, _ := LoadManifest(claudeDir); len(entries) != 0 {
			t.Errorf("a removal that did not happen is recorded: %+v", entries)
		}
		if backups, _ := os.ReadDir(filepath.Join(reapedDir(claudeDir), "backups")); len(backups) != 0 {
			t.Errorf("a backup of a removal that did not happen stayed: %v", backups)
		}
	})
	t.Run("delete", func(t *testing.T) {
		claudeDir := t.TempDir()
		cfg := filepath.Join(claudeDir, "claude.json")
		mustWrite(t, cfg, oursConfig)
		claudeWritesMeanwhile(t, cfg, theirsConfig)

		if err := DeleteMCP(claudeDir, cfg, "", "gone"); !errors.Is(err, ErrConfigChanged) {
			t.Fatalf("DeleteMCP = %v, want ErrConfigChanged", err)
		}
		if got := readString(t, cfg); got != theirsConfig {
			t.Errorf("the config was overwritten: %s", got)
		}
	})
	t.Run("restore", func(t *testing.T) {
		claudeDir := t.TempDir()
		cfg := filepath.Join(claudeDir, "claude.json")
		mustWrite(t, cfg, oursConfig)
		e, err := RemoveMCP(claudeDir, cfg, "", "gone")
		if err != nil {
			t.Fatal(err)
		}
		after := readString(t, cfg)
		theirs := after[:len(after)-1] + `, "numStartups": 43}`
		claudeWritesMeanwhile(t, cfg, theirs)

		if err := Restore(claudeDir, e.ID); !errors.Is(err, ErrConfigChanged) {
			t.Fatalf("Restore = %v, want ErrConfigChanged", err)
		}
		if got := readString(t, cfg); got != theirs {
			t.Errorf("the config was overwritten: %s", got)
		}
		beforeConfigWrite = func() {}
		if err := Restore(claudeDir, e.ID); err != nil { // run again: it reads the new file
			t.Fatalf("a second restore: %v", err)
		}
	})
}

func TestAConfigThatVanishesUnderAnEditIsAnError(t *testing.T) {
	claudeDir := t.TempDir()
	cfg := filepath.Join(claudeDir, "claude.json")
	mustWrite(t, cfg, oursConfig)
	old := beforeConfigWrite
	t.Cleanup(func() { beforeConfigWrite = old })
	beforeConfigWrite = func() { _ = os.Rename(cfg, cfg+".moved") }
	if err := DeleteMCP(claudeDir, cfg, "", "gone"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("DeleteMCP = %v, want the file's absence", err)
	}
	if _, err := os.Stat(cfg); !os.IsNotExist(err) {
		t.Error("a config that went away was written back")
	}
}
