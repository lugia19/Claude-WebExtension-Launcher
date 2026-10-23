//go:build windows

package patcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionedAppFolders(t *testing.T) {
	old := installBaseDir
	installBaseDir = t.TempDir()
	coworkRemovals := 0
	removeCoworkService = func() { coworkRemovals++ }
	defer func() { installBaseDir, removeCoworkService = old, RemoveCoworkService }()
	join := func(name string) string { return filepath.Join(installBaseDir, name) }
	mkApp := func(name string) {
		os.MkdirAll(join(name), 0755)
		os.WriteFile(filepath.Join(join(name), "claude.exe"), []byte("exe"), 0644)
	}

	// Before versioned folders: app-latest.
	if got := currentAppFolder(); got != join(appFolderName) {
		t.Fatalf("no current-app.txt: got %s, want app-latest", got)
	}
	mkApp(appFolderName)

	// A new version gets its own folder, and becomes current once committed.
	folder := newAppFolder("1.2.3")
	if folder != join("app-1.2.3") {
		t.Fatalf("newAppFolder = %s", folder)
	}
	mkApp("app-1.2.3")
	if got, err := commitAppFolder(folder, currentAppFolder()); err != nil || got != folder {
		t.Fatalf("commitAppFolder = %s, %v", got, err)
	}
	if got := currentAppFolder(); got != folder {
		t.Fatalf("after commit: got %s, want %s", got, folder)
	}

	// Reinstalling the same version never reuses the current folder.
	if again := newAppFolder("1.2.3"); again != join("app-1.2.3-2") {
		t.Fatalf("reinstall folder = %s", again)
	}

	// Cleanup keeps the current folder and one whose claude.exe is in use (an open
	// handle stands in for a running Claude), and removes the rest.
	mkApp("app-1.2.2")
	os.MkdirAll(join("app-1.2.3-2"), 0755) // a failed build's leftover, no claude.exe
	busy, err := os.Open(filepath.Join(join("app-1.2.2"), "claude.exe"))
	if err != nil {
		t.Fatal(err)
	}
	CleanupOldInstalls()
	busy.Close()

	for name, want := range map[string]bool{"app-1.2.3": true, "app-1.2.2": true, appFolderName: false, "app-1.2.3-2": false} {
		if _, err := os.Stat(join(name)); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", name, err == nil, want)
		}
	}
	if _, err := os.Stat(filepath.Join(join("app-1.2.2"), "claude.exe")); err != nil {
		t.Errorf("the in-use install lost its claude.exe: %v", err)
	}
	if coworkRemovals != 1 {
		t.Errorf("Cowork service removal ran %d times while app-latest was there, want 1", coworkRemovals)
	}

	// Once it's no longer in use, the next run removes it.
	CleanupOldInstalls()
	if _, err := os.Stat(join("app-1.2.2")); err == nil {
		t.Error("app-1.2.2 still there after it was closed")
	}
	if coworkRemovals != 1 {
		t.Error("Cowork service removal ran again with app-latest gone")
	}
}
