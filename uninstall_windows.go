package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"

	"claude-webext-patcher/patcher"
	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
)

const hasSharedSessions = true

// uninstallKeyPath is the launcher's entry in Settings > Apps > Installed apps (per
// user, so no admin rights are needed).
const uninstallKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ClaudeWebExtLauncher`

// registerInstall lists the installed launcher under Installed apps, with an
// Uninstall button that runs it with --uninstall. Rewritten on every start, so it
// follows the version.
func registerInstall(installed string) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKeyPath, registry.SET_VALUE)
	if err != nil {
		fmt.Printf("Warning: could not register in Installed apps: %v\n", err)
		return
	}
	defer key.Close()
	for name, value := range map[string]string{
		"DisplayName":     "Claude Desktop (Extended)",
		"DisplayIcon":     installed,
		"DisplayVersion":  Version,
		"Publisher":       "lugia19",
		"InstallLocation": filepath.Dir(installed),
		"UninstallString": `"` + installed + `" --uninstall`,
		"URLInfoAbout":    "https://github.com/lugia19/Claude-WebExtension-Launcher",
	} {
		key.SetStringValue(name, value)
	}
	key.SetDWordValue("NoModify", 1)
	key.SetDWordValue("NoRepair", 1)
}

// unshareSessions gives every Claude folder linked to the shared session store (the
// official Claude's, and the instances') a real folder again, holding a copy of the
// sessions, then removes the store. Nothing is deleted while anything still links to
// it, so the official Claude keeps its sessions.
func unshareSessions(instances []string) error {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil
	}
	lock, locked := utils.AcquirePatchLock(sessionLockName, sessionLockTimeout)
	if !locked {
		return fmt.Errorf("another launcher is changing the shared sessions")
	}
	defer lock.Release()

	roots := []string{filepath.Join(appData, "Claude")}
	for _, name := range instances {
		roots = append(roots, claudeUserDataDir(name))
	}
	for _, root := range roots {
		for _, folder := range sharedSessionFolders {
			if err := unshareFolder(filepath.Join(root, folder)); err != nil {
				return err
			}
		}
	}
	store := filepath.Join(appData, "ClaudeWebExtLauncher", "shared-sessions")
	if err := os.RemoveAll(store); err != nil {
		return fmt.Errorf("removing %s: %w", store, err)
	}
	return nil
}

// unshareFolder replaces the junction link (if it is one) with a real folder holding a
// copy of what it pointed at. Removing a junction removes only the link, never its
// target.
func unshareFolder(link string) error {
	target, err := os.Readlink(link)
	if err != nil {
		return nil // not a link (or not there): nothing to undo
	}
	fmt.Printf("Unsharing %s (was linked to %s)\n", link, target)
	if err := os.Remove(link); err != nil {
		return fmt.Errorf("removing the link %s: %w", link, err)
	}
	if _, err := os.Stat(target); err != nil {
		return os.MkdirAll(link, 0755)
	}
	if err := mergeTree(target, link); err != nil {
		return fmt.Errorf("copying the sessions into %s: %w", link, err)
	}
	return nil
}

// safeToDelete reports whether dir holds no session junction, which RemoveAll could
// otherwise follow into the shared store.
func safeToDelete(dir string) bool {
	for _, folder := range sharedSessionFolders {
		if _, err := os.Readlink(filepath.Join(dir, folder)); err == nil {
			return false
		}
	}
	return true
}

// removePatchedClaude runs the elevated worker (one UAC prompt) to remove the
// WindowsApps install and our Cowork service and firewall rules. The worker reports
// the row itself.
func removePatchedClaude(logPath string) error {
	if _, err := os.Stat(utils.WindowsInstallDir); err != nil && !coworkFirewallRulesExist() {
		ui.SetRow(rowRemoveClaude, status.Skipped, "", "not installed")
		return nil
	}
	res := followWorker([]string{"--uninstall", "--log-file=" + logPath})
	switch {
	case res.err != nil:
		return fmt.Errorf("removing the patched Claude needs administrator permission: %v", res.err)
	case res.code != 0:
		detail := res.failed[status.StepRemoveClaude]
		if detail == "" {
			detail = "see the log"
		}
		return fmt.Errorf("couldn't remove the patched Claude: %s", detail)
	}
	return nil
}

func coworkFirewallRulesExist() bool {
	return utils.Command("netsh", "advfirewall", "firewall", "show", "rule", "name=ClaudeWebExtLauncher-Cowork-In").Run() == nil
}

// uninstallWorker is the elevated part (worker --uninstall).
func uninstallWorker(st *status.Writer) int {
	st.Report(status.StepRemoveClaude, status.Running, "")
	if err := patcher.TakeWindowsAppsOwnership(); err != nil {
		fmt.Printf("Could not get access to WindowsApps: %v\n", err)
		st.Report(status.StepRemoveClaude, status.Failed, err.Error())
		return 1
	}
	defer patcher.ReleaseWindowsAppsOwnership()

	patcher.RemoveCoworkService()
	fmt.Printf("Removing %s\n", utils.WindowsInstallDir)
	if err := os.RemoveAll(utils.WindowsInstallDir); err != nil {
		fmt.Printf("Could not remove %s: %v\n", utils.WindowsInstallDir, err)
		st.Report(status.StepRemoveClaude, status.Failed, err.Error())
		return 1
	}
	st.Report(status.StepRemoveClaude, status.Done, "")
	return 0
}

// removeRegistrations removes the Start Menu and Startup shortcuts, the claude://
// handler (only if it's the patched Claude's), and the Installed apps entry.
func removeRegistrations() error {
	var problems []string
	for _, dir := range []string{startMenuDir(), filepath.Join(startMenuDir(), "Startup")} {
		lnks, _ := filepath.Glob(filepath.Join(dir, "Claude (*).lnk"))
		lnks = append(lnks, filepath.Join(dir, entryName(launcherEntry)+".lnk"))
		for _, lnk := range lnks {
			if err := removeIfExists(lnk); err != nil {
				problems = append(problems, err.Error())
			}
		}
	}

	if key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\claude\shell\open\command`, registry.QUERY_VALUE); err == nil {
		command, _, _ := key.GetStringValue("")
		key.Close()
		if strings.Contains(command, "ClaudeWebExtLauncher") {
			fmt.Println("Removing the claude:// handler (it pointed at the patched Claude)")
			if err := deleteKeyTree(registry.CURRENT_USER, `Software\Classes\claude`); err != nil {
				problems = append(problems, "claude:// handler: "+err.Error())
			}
		}
	}

	if err := registry.DeleteKey(registry.CURRENT_USER, uninstallKeyPath); err != nil && err != registry.ErrNotExist {
		problems = append(problems, "Installed apps entry: "+err.Error())
	}
	if len(problems) > 0 {
		return fmt.Errorf("some couldn't be removed: %s", strings.Join(problems, "; "))
	}
	return nil
}

// deleteKeyTree deletes a registry key and everything under it.
func deleteKeyTree(root registry.Key, path string) error {
	key, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	subkeys, _ := key.ReadSubKeyNames(-1)
	key.Close()
	for _, sub := range subkeys {
		if err := deleteKeyTree(root, path+`\`+sub); err != nil {
			return err
		}
	}
	return registry.DeleteKey(root, path)
}

// removeLauncherFiles removes the launcher's folders: its per-user data (%APPDATA%,
// the rest of the sessions store's parent) and %LOCALAPPDATA% except the running exe,
// which can't delete itself; finishUninstall removes that once this process is gone.
func removeLauncherFiles() error {
	os.RemoveAll(filepath.Join(os.Getenv("APPDATA"), "ClaudeWebExtLauncher"))

	dir := utils.DataDir()
	running, _ := runningLauncher()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // already gone
	}
	var failed []string
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if samePath(path, running) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			failed = append(failed, e.Name())
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("couldn't delete %s in %s", strings.Join(failed, ", "), dir)
	}
	return nil
}

// finishUninstall removes the launcher's folder once this process has exited: a hidden
// cmd waits a couple of seconds, then deletes it.
func finishUninstall() {
	dir := utils.DataDir()
	if _, err := os.Stat(dir); err != nil {
		return
	}
	cmd := utils.Command("cmd")
	cmd.SysProcAttr.CmdLine = fmt.Sprintf(`cmd /c ping -n 3 127.0.0.1 >nul & rmdir /s /q "%s"`, dir)
	if err := cmd.Start(); err != nil {
		fmt.Printf("Warning: could not schedule removing %s: %v\n", dir, err)
	}
}

// launcherProcessRunning reports whether a launcher process other than this one runs.
func launcherProcessRunning() bool {
	out, err := utils.Command("tasklist", "/fi", "imagename eq Claude_WebExtension_Launcher.exe", "/fo", "csv", "/nh").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		if pid, err := strconv.Atoi(strings.Trim(fields[1], "\" \r")); err == nil && pid != os.Getpid() {
			return true
		}
	}
	return false
}
