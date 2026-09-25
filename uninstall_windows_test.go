package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUnshareFolder: a session folder linked into the shared store becomes a real
// folder with the store's files, and the store itself is left intact.
func TestUnshareFolder(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	if err := os.MkdirAll(filepath.Join(store, "session1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "session1", "data.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "Claude", "local-agent-mode-sessions")
	if err := makeJunction(link, store); err != nil {
		t.Skipf("can't make junctions here: %v", err)
	}
	if safeToDelete(filepath.Dir(link)) {
		t.Error("a folder holding a session junction was considered safe to delete")
	}

	if err := unshareFolder(link, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(link); err == nil {
		t.Fatal("still a link")
	}
	if _, err := os.Stat(filepath.Join(link, "session1", "data.json")); err != nil {
		t.Fatalf("the sessions weren't copied back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "session1", "data.json")); err != nil {
		t.Fatalf("the store was touched: %v", err)
	}
	if !safeToDelete(filepath.Dir(link)) {
		t.Error("an unshared folder wasn't considered safe to delete")
	}
	if err := unshareFolder(link, true); err != nil { // a real folder: nothing to do
		t.Fatal(err)
	}
}
