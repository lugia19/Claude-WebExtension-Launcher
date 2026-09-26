package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"claude-webext-patcher/utils"
)

// removeSandbox: there's no AppArmor on macOS.
func removeSandbox() error { return nil }

// removeRegistrations removes the instances' apps in ~/Applications and the login
// entries (see shortcuts_darwin.go). claude:// is registered by the patched Claude's
// app bundle itself, and goes with it.
func removeRegistrations() error {
	var problems []string
	for _, app := range menuApps() {
		if err := os.RemoveAll(app); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, label := range agentLabels() {
		if err := removeAgent(label); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("some couldn't be removed: %s", strings.Join(problems, "; "))
	}
	return nil
}

// removeLauncherFiles removes the installed launcher app and the launcher's data
// folder (a running app can be deleted on macOS).
func removeLauncherFiles() (string, error) {
	for _, path := range []string{installedLauncher(), utils.DataDir()} {
		if err := os.RemoveAll(path); err != nil {
			return "", fmt.Errorf("removing %s: %w", path, err)
		}
	}
	return "", nil
}

// launcherProcessRunning reports whether a launcher process other than this one runs.
func launcherProcessRunning() bool {
	out, err := exec.Command("pgrep", "-x", launcherProcessName()).Output()
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
