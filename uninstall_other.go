//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"claude-webext-patcher/patcher"
	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
)

// uninstallWorker: only Windows needs an elevated uninstall step.
func uninstallWorker(st *status.Writer) int { return 0 }

// finishUninstall: nothing left for later; a running program can be deleted here.
func finishUninstall() {}

// removePatchedClaude removes the patched Claude and its extensions, and the AppArmor
// profile the launcher installed for it on Linux (as root: asks for the password).
func removePatchedClaude(logPath string) (string, error) {
	for _, dir := range []string{patcher.AppFolder, utils.ResolveInstallPath("web-extensions")} {
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("removing %s: %w", dir, err)
		}
	}
	if err := removeSandbox(); err != nil {
		return "", asWarning(fmt.Errorf("AppArmor profile left: %w", err))
	}
	return "", nil
}

// launcherProcessName is the launcher's executable name, as process lists show it.
func launcherProcessName() string {
	return filepath.Base(launcherBinary(installedLauncher()))
}
