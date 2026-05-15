//go:build !windows

package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// prepareAdminContext relaunches the launcher inside Terminal.app on macOS
// when there is no controlling terminal, so console output is visible.
// On other non-Windows platforms it is a no-op.
func prepareAdminContext() error {
	if runtime.GOOS == "darwin" && os.Getenv("TERM") == "" {
		executable, _ := os.Executable()
		execDir := filepath.Dir(executable)

		// Change to the executable's directory, run, then exit terminal
		// Escape single quotes in paths for AppleScript
		execDirEscaped := strings.ReplaceAll(execDir, `'`, `'\''`)
		executableEscaped := strings.ReplaceAll(executable, `'`, `'\''`)
		script := fmt.Sprintf(`tell application "Terminal"
			set newTab to do script "cd '%s' && '%s' && exit"
			activate
		end tell`, execDirEscaped, executableEscaped)

		cmd := exec.Command("osascript", "-e", script)
		cmd.Start()
		os.Exit(0)
	}
	return nil
}

// releaseAdminContext is a no-op on non-Windows platforms.
func releaseAdminContext() {}

func claudeUserDataDir(instance string) string {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "Claude-"+instance)
	}
	return ""
}

func claudeExecutablePath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(patcher.AppFolder, "Claude.app", "Contents", "MacOS", "Claude")
	}
	// Linux and other Unix-like systems
	return filepath.Join(patcher.AppFolder, "claude")
}

// claudeInstalled returns true if the Claude executable exists in the install directory.
func claudeInstalled() bool {
	_, err := os.Stat(claudeExecutablePath())
	return err == nil
}

// ensureClaudeReady runs patching and extension updates in-process on macOS.
func ensureClaudeReady(forceUpdate bool) error {
	if err := patcher.EnsurePatched(forceUpdate); err != nil {
		if claudeInstalled() {
			fmt.Printf("Warning: patching failed (%v), launching existing installation.\n", err)
		} else {
			return err
		}
	}
	if err := extensions.UpdateAll(); err != nil {
		fmt.Printf("Warning: extension update failed: %v\n", err)
	}
	if err := patcher.DeploySentinelExtension(); err != nil {
		fmt.Printf("Warning: sentinel extension deployment failed: %v\n", err)
	}
	return nil
}

// runPatcherMode is not used on non-Windows platforms.
func runPatcherMode(forceUpdate bool) int {
	fmt.Println("--patcher is not supported on this platform")
	return 1
}
