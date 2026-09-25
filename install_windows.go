package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows/registry"

	"claude-webext-patcher/utils"
)

// uninstallKeyPath is the launcher's entry in Settings > Apps > Installed apps (and
// Programs and Features); per user, so no admin rights are needed.
const uninstallKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ClaudeWebExtLauncher`

// registerInstall lists the installed launcher under Installed apps, with an
// Uninstall button that runs it with --uninstall. Checked on every start of the
// installed copy, and only written when it's missing or out of date.
func registerInstall(installed string) {
	uninstall := `"` + installed + `" --uninstall`
	key, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		fmt.Printf("Warning: could not register in Installed apps: %v\n", err)
		return
	}
	defer key.Close()
	version, _, _ := key.GetStringValue("DisplayVersion")
	current, _, _ := key.GetStringValue("UninstallString")
	if version == Version && current == uninstall {
		return
	}
	key.SetStringValue("DisplayName", "Claude Desktop (Extended)")
	key.SetStringValue("DisplayIcon", installed)
	key.SetStringValue("DisplayVersion", Version)
	key.SetStringValue("Publisher", "lugia19")
	key.SetStringValue("InstallLocation", filepath.Dir(installed))
	key.SetStringValue("UninstallString", uninstall)
	key.SetStringValue("URLInfoAbout", "https://github.com/lugia19/Claude-WebExtension-Launcher")
	key.SetDWordValue("NoModify", 1)
	key.SetDWordValue("NoRepair", 1)
}

// installedLauncher is where the launcher installs itself: its per-user folder, next to
// the logs and settings.
func installedLauncher() string {
	return filepath.Join(utils.DataDir(), "Claude_WebExtension_Launcher.exe")
}

// runningLauncher is the exe this process runs from.
func runningLauncher() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// launcherBinary is the file holding the launcher's code: the exe itself here.
func launcherBinary(path string) string { return path }

func installCopy(running, installed string) error {
	return replaceFile(running, installed, 0755)
}

// handOff starts the installed launcher with args and exits. With --debug it waits
// instead, so the installed copy can use this console (it attaches to its parent's),
// and exits with its exit code. Returns only if the installed copy couldn't start.
func handOff(target string, args []string, debug bool) error {
	cmd := exec.Command(target, args...)
	cmd.Dir = filepath.Dir(target)
	if debug {
		if err := cmd.Run(); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				os.Exit(exit.ExitCode())
			}
			return err
		}
		os.Exit(0)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
