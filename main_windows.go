//go:build windows

package main

import (
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// patchLockName serializes the elevated worker across concurrently-launched
	// instances (all run as the same user in one session, so Local\ is shared).
	patchLockName = `Local\ClaudeWebExtLauncher-Patch`
	// patchLockTimeout is generous: a real update downloads the ~222 MB MSIX.
	patchLockTimeout = 5 * time.Minute
)

// platformSetup cleans up files older launcher versions left next to the executable:
// this one, and the downloaded copy that handed over to it (installedFrom), which is
// where those versions ran from.
func platformSetup(installedFrom string) {
	dirs := []string{utils.GetExecutableDir()}
	if installedFrom != "" {
		dirs = append(dirs, filepath.Dir(installedFrom))
	}
	for _, dir := range dirs {
		for _, oldDir := range []string{"app-latest", "web-extensions"} {
			oldPath := filepath.Join(dir, oldDir)
			if _, err := os.Stat(oldPath); err == nil {
				fmt.Printf("Removing old %s from %s...\n", oldDir, dir)
				if err := os.RemoveAll(oldPath); err != nil {
					fmt.Printf("Warning: could not remove %s: %v\n", oldPath, err)
				}
			}
		}
	}
}

func claudeUserDataDir(instance string) string {
	return filepath.Join(os.Getenv("APPDATA"), "Claude-"+instance)
}

func claudeExecutablePath() string {
	return filepath.Join(patcher.AppFolder, "claude.exe")
}

// detachFromTerminal is a no-op here; see main_linux.go.
func detachFromTerminal(cmd *exec.Cmd) {}

// ensureConsole attaches or opens a console for output (the build is GUI-subsystem).
func ensureConsole() {
	utils.EnsureConsole()
}

// The sandbox (AppArmor) step is Linux-only.
func sandboxNeeded() bool   { return false }
func installSandbox() error { return nil }

// coworkNeeded reports whether the Cowork service still has to be registered.
func coworkNeeded() bool {
	return !patcher.CoworkServiceExists()
}

// startWorker runs the worker elevated through UAC (the install lives in
// C:\Program Files\WindowsApps) and returns its exit code. The build is GUI-subsystem,
// so the worker opens no window; it writes to the log file itself.
func startWorker(args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return -1, err
	}
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	return utils.RunElevatedAndWait(exe, strings.Join(quoted, " "))
}

// workerBefore gives the elevated worker access to the WindowsApps folder.
func workerBefore() error {
	return patcher.TakeWindowsAppsOwnership()
}

// workerAfter lets the unelevated launcher read and run the install, and hands
// WindowsApps back to TrustedInstaller.
func workerAfter() {
	patcher.GrantUserReadAccess()
	patcher.ReleaseWindowsAppsOwnership()
}

func registerCowork() error {
	return patcher.RegisterCoworkService()
}
