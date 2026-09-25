package main

import (
	"os"
	"path/filepath"
	"syscall"

	"claude-webext-patcher/utils"
)

// installedLauncher is where the launcher installs itself: its data folder (with the
// patched Claude, logs and settings).
func installedLauncher() string {
	return filepath.Join(utils.DataDir(), "Claude_WebExtension_Launcher")
}

// runningLauncher is the binary this process runs from.
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

// launcherBinary is the file holding the launcher's code: the binary itself here.
func launcherBinary(path string) string { return path }

func samePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

func installCopy(running, installed string) error {
	return replaceFile(running, installed, 0755)
}

// handOff replaces this process with the installed launcher. Returns only if that
// failed.
func handOff(target string, args []string, debug bool) error {
	return syscall.Exec(target, append([]string{target}, args...), os.Environ())
}
