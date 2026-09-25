package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"claude-webext-patcher/patcher"
	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
)

const hasSharedSessions = false

func registerInstall(installed string)         {}
func unshareSessions(instances []string) error { return nil }
func safeToDelete(dir string) bool             { return true }
func uninstallWorker(st *status.Writer) int    { return 0 }
func finishUninstall()                         {}

// removePatchedClaude removes the patched Claude and its extensions.
func removePatchedClaude(logPath string) error {
	for _, dir := range []string{patcher.AppFolder, utils.ResolvePath("web-extensions")} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("removing %s: %w", dir, err)
		}
	}
	ui.SetRow(rowRemoveClaude, status.Done, "", "")
	return nil
}

// removeRegistrations: the launcher makes no shortcuts on macOS. (claude:// is
// registered by the app bundle itself, and goes with it.)
func removeRegistrations() error { return nil }

// removeLauncherFiles removes the installed launcher app and the launcher's data
// folder (a running app can be deleted on macOS).
func removeLauncherFiles() error {
	for _, path := range []string{installedLauncher(), utils.DataDir()} {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}
	return nil
}

// launcherProcessRunning reports whether a launcher process other than this one runs.
func launcherProcessRunning() bool {
	out, err := exec.Command("pgrep", "-x", "Claude_WebExtension_Launcher").Output()
	if err != nil {
		return false
	}
	for _, field := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(field); err == nil && pid != os.Getpid() {
			return true
		}
	}
	return false
}
