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
	defer func() { installBaseDir = old }()
	join := func(name string) string { return filepath.Join(installBaseDir, name) }

	// Before versioned folders: app-latest.
	if got := currentAppFolder(); got != join(appFolderName) {
		t.Fatalf("no current-app.txt: got %s, want app-latest", got)
	}
	os.MkdirAll(join(appFolderName), 0755)

	// A new version gets its own folder, and becomes current once committed.
	folder := newAppFolder("1.2.3")
	if folder != join("app-1.2.3") {
		t.Fatalf("newAppFolder = %s", folder)
	}
	os.MkdirAll(folder, 0755)
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
}

func TestCleanupOldInstalls(t *testing.T) {
	oldBase, oldProcs, oldMove, oldStop := installBaseDir, appFolderProcesses, moveCowork, stopCowork
	defer func() {
		installBaseDir, appFolderProcesses, moveCowork, stopCowork = oldBase, oldProcs, oldMove, oldStop
	}()
	installBaseDir = t.TempDir()
	join := func(name string) string { return filepath.Join(installBaseDir, name) }
	exists := func(name string) bool { _, err := os.Stat(join(name)); return err == nil }

	// What runs where: Claude from app-1, our Cowork service from app-latest.
	running := map[string]map[string]bool{
		"app-1":      {"claude.exe": true},
		"app-latest": {"cowork-svc.exe": true},
	}
	appFolderProcesses = func() map[string]map[string]bool { return running }
	movedTo, stops := "", 0
	moveCowork = func(folder string) { movedTo = folder }
	stopCowork = func() { stops++; delete(running, "app-latest") }

	for _, name := range []string{"app-current", "app-1", "app-latest", "app-leftover"} {
		os.MkdirAll(join(name), 0755)
	}
	commitAppFolder(join("app-current"), "")

	// Claude is open: app-1 is in use, and the service isn't stopped under it, so
	// app-latest stays too. Only the unused leftover goes.
	CleanupOldInstalls()
	if movedTo != join("app-current") {
		t.Errorf("Cowork service moved to %q, want app-current", movedTo)
	}
	if !exists("app-current") || !exists("app-1") || !exists("app-latest") || exists("app-leftover") {
		t.Errorf("with Claude open: current %v, app-1 %v, app-latest %v, leftover %v",
			exists("app-current"), exists("app-1"), exists("app-latest"), exists("app-leftover"))
	}
	if stops != 0 {
		t.Error("stopped the Cowork service while Claude was open")
	}

	// Claude closed: the service is stopped to free app-latest, and both old ones go.
	delete(running, "app-1")
	CleanupOldInstalls()
	if stops != 1 || exists("app-1") || exists("app-latest") || !exists("app-current") {
		t.Errorf("with Claude closed: stops %d, app-1 %v, app-latest %v, current %v",
			stops, exists("app-1"), exists("app-latest"), exists("app-current"))
	}
}
