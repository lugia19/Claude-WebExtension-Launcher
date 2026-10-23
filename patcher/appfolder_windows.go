//go:build windows

package patcher

import (
	"claude-webext-patcher/utils"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Each install goes into a folder of its own, app-<version> (app-<version>-2, ... when
// that name is taken), and currentAppFile names the current one. Windows can't rename
// or replace a folder while anything in it is in use, so swapping a fixed folder fails
// whenever Claude (or anything else) still has a file of the old install open. A new
// folder needs nothing from the old one; CleanupOldInstalls deletes old ones in a
// later worker run, once they're no longer in use.
const currentAppFile = "current-app.txt"

// removeCoworkService is RemoveCoworkService, replaceable in tests.
var removeCoworkService = RemoveCoworkService

// currentAppFolder is the current install's folder. Installs from before versioned
// folders have no currentAppFile: they're app-latest.
func currentAppFolder() string {
	if name := readVersionFile(currentAppFile); strings.HasPrefix(name, "app-") && filepath.Base(name) == name {
		return filepath.Join(installBaseDir, name)
	}
	return filepath.Join(installBaseDir, appFolderName)
}

// newAppFolder is where buildAndInstall builds version: a folder that doesn't exist
// yet, so never the current install, even when reinstalling the same version.
func newAppFolder(version string) string {
	base := filepath.Join(installBaseDir, "app-"+version)
	folder := base
	for n := 2; ; n++ {
		if _, err := os.Lstat(folder); errors.Is(err, fs.ErrNotExist) {
			return folder
		}
		folder = fmt.Sprintf("%s-%d", base, n)
	}
}

// commitAppFolder makes folder the current install, by naming it in currentAppFile,
// and returns it. The previous install stays where it is.
func commitAppFolder(folder, previous string) (string, error) {
	path := filepath.Join(installBaseDir, currentAppFile)
	if err := utils.WriteFileAtomic(path, []byte(filepath.Base(folder)), 0644); err != nil {
		return previous, fmt.Errorf("recording the new install: %w", err)
	}
	fmt.Printf("Installed to %s\n", folder)
	return folder, nil
}

// CleanupOldInstalls deletes every install folder but the current one: earlier
// versions, app-latest from before versioned folders, and leftovers of failed builds.
// claude.exe goes first: Windows won't delete it while it runs, so a folder that an
// open Claude still uses is skipped (and retried next time) before anything in it is
// touched. Must be called elevated.
//
// While app-latest is still there, the Cowork service older versions registered is
// removed first: it runs from app-latest (and would keep it from being deleted).
func CleanupOldInstalls() {
	current := filepath.Base(currentAppFolder())
	if !strings.EqualFold(current, appFolderName) {
		if _, err := os.Stat(filepath.Join(installBaseDir, appFolderName)); err == nil {
			removeCoworkService()
		}
	}
	entries, err := os.ReadDir(installBaseDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "app-") || strings.EqualFold(name, current) {
			continue
		}
		dir := filepath.Join(installBaseDir, name)
		if err := os.Remove(filepath.Join(dir, "claude.exe")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fmt.Printf("Keeping old install %s for now, it's in use: %v\n", name, err)
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			fmt.Printf("Could not fully remove old install %s (will retry next time): %v\n", name, err)
			continue
		}
		fmt.Printf("Removed old install %s\n", name)
	}
}
