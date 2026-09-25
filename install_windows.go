package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"claude-webext-patcher/utils"
)

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

// installCopy installs the running exe, and the uninstall script shipped next to it
// (if any), into the install folder.
func installCopy(running, installed string) error {
	if err := replaceFile(running, installed, 0755); err != nil {
		return err
	}
	bat := filepath.Join(filepath.Dir(running), "Uninstall.bat")
	if _, err := os.Stat(bat); err == nil {
		copyWithMode(bat, filepath.Join(filepath.Dir(installed), "Uninstall.bat"), 0644)
	}
	return nil
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
