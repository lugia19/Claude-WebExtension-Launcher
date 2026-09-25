//go:build !windows

package patcher

import "claude-webext-patcher/utils"

func initPaths() {
	installBaseDir = utils.ResolvePath(".")
	setAppPaths(utils.ResolvePath(appFolderName))
}

// prepareInstallDir is a no-op on non-Windows platforms.
func prepareInstallDir() error {
	return nil
}

// CoworkServiceExists is Windows-only; on other platforms report "present" so the shared
// launcher flow never tries to register a service.
func CoworkServiceExists() bool {
	return true
}
