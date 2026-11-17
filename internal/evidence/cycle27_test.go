package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

// A record of past uses that is there and cannot be read is not a first run:
// it read as empty, with no word, and the next run wrote over it.
func TestLoadCheckedTellsAnUnreadableRecordFromNone(t *testing.T) {
	dir := t.TempDir()
	if d, err := LoadChecked(filepath.Join(dir, "none.json")); err != nil || d == nil {
		t.Errorf("no file: %v", err)
	}
	for name, body := range map[string]string{"cut.json": `{"format":1,"sources":{"a"`, "newer.json": `{"format":999,"sources":{},"items":{}}`} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if d, err := LoadChecked(path); err == nil || d == nil {
			t.Errorf("%s: err %v", name, err)
		}
	}
	good := filepath.Join(dir, "good.json")
	if err := Save(good, New()); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadChecked(good); err != nil {
		t.Errorf("a record just saved: %v", err)
	}
}
