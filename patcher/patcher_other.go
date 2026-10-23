//go:build !windows

package patcher

import (
	"claude-webext-patcher/utils"
	"fmt"
	"os"
)

func initPaths() {
	installBaseDir = utils.ResolvePath(".")
	setAppPaths(utils.ResolvePath(appFolderName))
}

// prepareInstallDir is a no-op on non-Windows platforms.
func prepareInstallDir() error {
	return nil
}

// newAppFolder is where buildAndInstall builds a new install: a staging folder next
// to app-latest (cleared of any leftover from an aborted run), which commitAppFolder
// swaps into place. The install stays at the same path, which the AppArmor profile
// (Linux) is keyed on. Renaming a folder with files in use works here, unlike on
// Windows.
func newAppFolder(version string) string {
	staging := AppFolder + ".staging"
	os.RemoveAll(staging)
	return staging
}

// commitAppFolder replaces target (app-latest) with staging via two renames
// (os.Rename cannot replace an existing directory) and returns target. The window
// where target is absent is a couple of fast metadata ops. If moving the old install
// aside fails, the old install is left in place.
func commitAppFolder(staging, target string) (string, error) {
	backup := target + ".old"
	os.RemoveAll(backup)

	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return target, fmt.Errorf("moving old install aside: %w", err)
		}
	}

	if err := os.Rename(staging, target); err != nil {
		// Roll the old install back so we're never left without one.
		if _, berr := os.Stat(backup); berr == nil {
			os.Rename(backup, target)
		}
		return target, fmt.Errorf("swapping in new install: %w", err)
	}

	os.RemoveAll(backup)
	return target, nil
}
