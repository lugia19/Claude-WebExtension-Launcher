package main

import (
	"claude-webext-patcher/patcher"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// prepareAdminContext relaunches the launcher inside Terminal.app when there is no
// controlling terminal, so console output is visible.
func prepareAdminContext() error {
	if !guiMode && os.Getenv("TERM") == "" {
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

func claudeUserDataDir(instance string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Claude-"+instance)
}

func claudeExecutablePath() string {
	return filepath.Join(patcher.AppFolder, "Claude.app", "Contents", "MacOS", "Claude")
}

// detachFromTerminal is a no-op here; see main_linux.go.
func detachFromTerminal(cmd *exec.Cmd) {}
