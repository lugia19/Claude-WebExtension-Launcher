package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"claude-webext-patcher/patcher"
	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
)

// removePatchedClaude runs the elevated worker (one UAC prompt) to remove the
// WindowsApps install and our Cowork service and firewall rules.
func removePatchedClaude(logPath string) (string, error) {
	if _, err := os.Stat(utils.WindowsInstallDir); err != nil && !patcher.CoworkFirewallRulesExist() {
		return "", stepSkipped("not installed")
	}
	res := followWorker([]string{"--uninstall", "--log-file=" + logPath})
	switch {
	case res.err != nil:
		return "", fmt.Errorf("removing the patched Claude needs administrator permission: %v", res.err)
	case res.code != 0:
		return "", fmt.Errorf("couldn't remove the patched Claude: %s", res.detail(status.StepRemoveClaude))
	}
	return "", nil
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
	for _, dir := range []string{startMenuDir(), filepath.Dir(startupShortcut(launcherEntry))} {
		lnks, _ := filepath.Glob(filepath.Join(dir, entryName("*")+".lnk"))
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

// removeLauncherFiles removes the launcher's roaming folder (the MSIX choice). Its
// local folder (%LOCALAPPDATA%\ClaudeWebExtLauncher) holds the running exe, which
// can't delete itself, so finishUninstall removes it once this process is gone.
func removeLauncherFiles() (string, error) {
	if err := os.RemoveAll(filepath.Join(os.Getenv("APPDATA"), "ClaudeWebExtLauncher")); err != nil {
		return "", err
	}
	return "the rest goes once this window is closed", nil
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
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snapshot)
	name := filepath.Base(launcherBinary(installedLauncher()))
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID != uint32(os.Getpid()) && strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
			return true
		}
	}
	return false
}
