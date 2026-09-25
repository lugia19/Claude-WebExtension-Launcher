package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"claude-webext-patcher/patcher"
	"claude-webext-patcher/utils"
)

// removeSandbox unloads and deletes the launcher's AppArmor profile, if there is one.
func removeSandbox() error {
	path := appArmorProfilePath()
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	fmt.Printf("Removing the AppArmor profile %s (needs your password)...\n", path)
	if err := runShAsRoot(`apparmor_parser -R "$1" 2>/dev/null; rm -f "$1"`, path); err != nil {
		return fmt.Errorf("%v (remove %s by hand)", err, path)
	}
	return nil
}

// removeRegistrations removes the menu and autostart entries, the claude:// handler
// entry, and its association in mimeapps.list.
func removeRegistrations() error {
	var problems []string
	for _, dir := range []string{applicationsDir(), autostartDir()} {
		entries, _ := filepath.Glob(filepath.Join(dir, desktopFilePrefix+"*.desktop"))
		for _, e := range entries {
			if err := removeIfExists(e); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	if err := removeIfExists(filepath.Join(applicationsDir(), patcher.LinkHandlerDesktop)); err != nil {
		problems = append(problems, err.Error())
	}
	mimeapps := filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "mimeapps.list")
	if data, err := os.ReadFile(mimeapps); err == nil {
		if updated := removeMimeHandler(string(data), patcher.LinkHandlerDesktop); updated != string(data) {
			if err := os.WriteFile(mimeapps, []byte(updated), 0644); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}
	refreshDesktopDatabase()
	if len(problems) > 0 {
		return fmt.Errorf("some couldn't be removed: %s", strings.Join(problems, "; "))
	}
	return nil
}

// removeLauncherFiles removes the launcher's data folder, the launcher included (a
// running binary can be deleted on Linux). Nothing may use the folder afterwards:
// ResolvePath and the log and lock helpers would create it again.
func removeLauncherFiles() (string, error) {
	dir := utils.DataDir()
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("removing %s: %w", dir, err)
	}
	return "", nil
}

// launcherProcessRunning reports whether a launcher process other than this one runs.
func launcherProcessRunning() bool {
	name := launcherProcessName()
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err == nil && filepath.Base(strings.TrimSuffix(exe, " (deleted)")) == name {
			return true
		}
	}
	return false
}
