package override

import (
	"testing"
)

func TestAddKeep(t *testing.T) {
	dir := t.TempDir()
	if err := AddKeep(dir, "skill:my-test-skill"); err != nil {
		t.Fatal(err)
	}
	items, err := ListKeep(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0] != "skill:my-test-skill" {
		t.Fatalf("got %v", items)
	}
}

func TestAddKeepDedup(t *testing.T) {
	dir := t.TempDir()
	if err := AddKeep(dir, "skill:x"); err != nil {
		t.Fatal(err)
	}
	if err := AddKeep(dir, "skill:x"); err != nil {
		t.Fatal(err)
	}
	items, _ := ListKeep(dir)
	if len(items) != 1 {
		t.Fatalf("dup should not add, got %d items", len(items))
	}
}

func TestRemoveKeep(t *testing.T) {
	dir := t.TempDir()
	_ = AddKeep(dir, "skill:a")
	_ = AddKeep(dir, "skill:b")
	if err := RemoveKeep(dir, "skill:a"); err != nil {
		t.Fatal(err)
	}
	items, _ := ListKeep(dir)
	if len(items) != 1 || items[0] != "skill:b" {
		t.Fatalf("got %v", items)
	}
}

func TestRemoveKeepNotFound(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveKeep(dir, "skill:nonexistent"); err == nil {
		t.Fatal("expected error")
	}
}

func TestKeepSet(t *testing.T) {
	dir := t.TempDir()
	_ = AddKeep(dir, "skill:a")
	_ = AddKeep(dir, "mcp:b")
	set, err := KeepSet(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !set["skill:a"] {
		t.Error("skill:a missing from set")
	}
	if !set["mcp:b"] {
		t.Error("mcp:b missing from set")
	}
	if set["skill:c"] {
		t.Error("skill:c should not be in set")
	}
}

func TestItemKey(t *testing.T) {
	if k := ItemKey("skill", "ecc:flox"); k != "skill:ecc:flox" {
		t.Fatalf("got %q", k)
	}
}

func TestEmptyList(t *testing.T) {
	dir := t.TempDir()
	items, err := ListKeep(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty, got %v", items)
	}
}
