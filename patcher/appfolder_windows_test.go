//go:build windows

package patcher

import (
	"os"
	"path/filepath"
	"slices"
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

// cleanupFixture stubs what CleanupOldInstalls asks Windows about: the processes
// running from each install folder, and our Cowork service.
type cleanupFixture struct {
	running       map[string]map[string]bool
	serviceFolder string // where moveCowork leaves the service ("" = not ours)
	stops         int
	firewall      []string // the programs the rules were last synced to
}

func newCleanupFixture(t *testing.T, folders ...string) *cleanupFixture {
	oldBase, oldProcs, oldMove, oldStop, oldSync := installBaseDir, appFolderProcesses, moveCowork, stopCowork, syncCoworkRules
	t.Cleanup(func() {
		installBaseDir, appFolderProcesses, moveCowork, stopCowork, syncCoworkRules = oldBase, oldProcs, oldMove, oldStop, oldSync
	})
	installBaseDir = t.TempDir()
	f := &cleanupFixture{running: map[string]map[string]bool{}}
	appFolderProcesses = func() map[string]map[string]bool { return f.running }
	moveCowork = func(string) string { return f.serviceFolder }
	stopCowork = func() {
		f.stops++
		for _, exes := range f.running {
			delete(exes, "cowork-svc.exe")
		}
	}
	syncCoworkRules = func(programs []string) error { f.firewall = slices.Sorted(slices.Values(programs)); return nil }
	for _, name := range folders {
		os.MkdirAll(filepath.Join(installBaseDir, name), 0755)
	}
	commitAppFolder(filepath.Join(installBaseDir, "app-current"), "")
	return f
}

func (f *cleanupFixture) exists(name string) bool {
	_, err := os.Stat(filepath.Join(installBaseDir, name))
	return err == nil
}

func svcExe(folder string) string {
	return filepath.Join(installBaseDir, folder, "resources", "cowork-svc.exe")
}

func TestCleanupOldInstalls(t *testing.T) {
	// Claude runs from app-1, our Cowork service (moved to app-current) still from app-latest.
	f := newCleanupFixture(t, "app-current", "app-1", "app-latest", "app-leftover")
	f.serviceFolder = "app-current"
	f.running["app-1"] = map[string]bool{"claude.exe": true}
	f.running["app-latest"] = map[string]bool{"cowork-svc.exe": true}

	// Claude is open: app-1 is in use, and the service isn't stopped under it, so
	// app-latest stays too, and keeps its firewall allowance. Only the leftover goes.
	CleanupOldInstalls()
	if !f.exists("app-current") || !f.exists("app-1") || !f.exists("app-latest") || f.exists("app-leftover") {
		t.Errorf("with Claude open: current %v, app-1 %v, app-latest %v, leftover %v",
			f.exists("app-current"), f.exists("app-1"), f.exists("app-latest"), f.exists("app-leftover"))
	}
	if f.stops != 0 {
		t.Error("stopped the Cowork service while Claude was open")
	}
	if want := []string{svcExe("app-current"), svcExe("app-latest")}; !slices.Equal(f.firewall, want) {
		t.Errorf("firewall with the old service running = %v, want %v", f.firewall, want)
	}

	// Claude closed: the service is stopped to free app-latest, both old ones go, and
	// the firewall rules are left with the current folder only.
	delete(f.running, "app-1")
	CleanupOldInstalls()
	if f.stops != 1 || f.exists("app-1") || f.exists("app-latest") || !f.exists("app-current") {
		t.Errorf("with Claude closed: stops %d, app-1 %v, app-latest %v, current %v",
			f.stops, f.exists("app-1"), f.exists("app-latest"), f.exists("app-current"))
	}
	if want := []string{svcExe("app-current")}; !slices.Equal(f.firewall, want) {
		t.Errorf("firewall after = %v, want %v", f.firewall, want)
	}
}

func TestCleanupKeepsServiceFolderWhenMoveFails(t *testing.T) {
	// Moving the service failed: it's still set up to run from app-old, which nothing
	// runs from right now. It must stay, or the service would point at a deleted exe.
	f := newCleanupFixture(t, "app-current", "app-old")
	f.serviceFolder = "app-old"
	CleanupOldInstalls()
	if !f.exists("app-old") {
		t.Error("deleted the folder the Cowork service is still set up to run from")
	}
	if want := []string{svcExe("app-old")}; !slices.Equal(f.firewall, want) {
		t.Errorf("firewall = %v, want %v", f.firewall, want)
	}
}

func TestCleanupWithoutOurService(t *testing.T) {
	// An official install's service (or none): our firewall rules aren't touched.
	f := newCleanupFixture(t, "app-current", "app-old")
	CleanupOldInstalls()
	if f.exists("app-old") || f.firewall != nil {
		t.Errorf("app-old %v, firewall synced to %v", f.exists("app-old"), f.firewall)
	}
}

func TestCoworkServiceFolder(t *testing.T) {
	old := installBaseDir
	installBaseDir = `C:\Program Files\WindowsApps\ClaudeWebExtLauncher`
	defer func() { installBaseDir = old }()
	config := "        BINARY_PATH_NAME   : C:\\Program Files\\WindowsApps\\ClaudeWebExtLauncher\\app-latest\\resources\\cowork-svc.exe\r\n"
	if got := coworkServiceFolder(config); got != "app-latest" {
		t.Errorf("coworkServiceFolder = %q, want app-latest", got)
	}
	if got := coworkServiceFolder(`BINARY_PATH_NAME : C:\Program Files\WindowsApps\Claude_1_x64\app\resources\cowork-svc.exe`); got != "" {
		t.Errorf("an official service's folder = %q, want \"\"", got)
	}
}
