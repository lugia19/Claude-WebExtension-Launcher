package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// removePatchedClaude removes the patched Claude and its extensions, and the AppArmor
// profile the launcher installed for it (as root, asking for the password once).
func removePatchedClaude(logPath string) error {
	for _, dir := range []string{patcher.AppFolder, utils.ResolvePath("web-extensions")} {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("removing %s: %w", dir, err)
		}
	}
	if err := removeSandbox(); err != nil {
		fmt.Printf("Warning: %v\n", err)
		ui.SetRow(rowRemoveClaude, status.Warning, "", "AppArmor profile left: "+err.Error())
		return nil
	}
	ui.SetRow(rowRemoveClaude, status.Done, "", "")
	return nil
}

// removeSandbox unloads and deletes the launcher's AppArmor profile, if there is one.
func removeSandbox() error {
	path := filepath.Join(appArmorDir, fmt.Sprintf("claude-webext-launcher-%d", os.Getuid()))
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	fmt.Printf("Removing the AppArmor profile %s (needs your password)...\n", path)
	script := `apparmor_parser -R "$1" 2>/dev/null; rm -f "$1"`
	shArgs := []string{"/bin/sh", "-c", script, "sh", path}
	err := errors.New("pkexec is not installed")
	if _, lookErr := exec.LookPath("pkexec"); lookErr == nil {
		if err = runAsRoot("pkexec", shArgs); err == nil {
			return nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 126 {
			return fmt.Errorf("password prompt was cancelled (remove %s by hand)", path)
		}
	}
	if stdinIsTerminal() {
		return runAsRoot("sudo", shArgs)
	}
	return fmt.Errorf("%v (remove %s by hand)", err, path)
}

// removeRegistrations removes the menu and autostart entries, the claude:// handler
// entry, and its association in mimeapps.list.
func removeRegistrations() error {
	var problems []string
	for _, dir := range []string{applicationsDir(), autostartDir()} {
		entries, _ := filepath.Glob(filepath.Join(dir, "claude-webext-launcher*.desktop"))
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
func removeLauncherFiles() error {
	dir := utils.DataDir()
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("removing %s: %w", dir, err)
	}
	return nil
}

// launcherProcessRunning reports whether a launcher process other than this one runs.
func launcherProcessRunning() bool {
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		exe, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			continue
		}
		if filepath.Base(strings.TrimSuffix(exe, " (deleted)")) == "Claude_WebExtension_Launcher" {
			return true
		}
	}
	return false
}
