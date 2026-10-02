package main

import (
	"claude-webext-patcher/patcher"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// platformSetup keeps the hidden claude:// link handler entry current (see
// shortcuts_linux.go), so magic links reach the patched app.
func platformSetup(installedFrom string) {
	writeLinkHandler()
}

// claudeUserDataDir mirrors Electron's appData on Linux: $XDG_CONFIG_HOME, defaulting
// to ~/.config. The wrapper appends "-<instance>" to the app name there.
func claudeUserDataDir(instance string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "Claude-"+instance)
}

func claudeExecutablePath() string {
	return filepath.Join(patcher.AppFolder, "claude-desktop")
}

// startClaude starts Claude in its own session, so closing the terminal the launcher
// ran in (which SIGHUPs its process group) doesn't take Claude down with it.
func startClaude(cmd *exec.Cmd, env []string) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// coworkNeeded is Windows-only.
func coworkNeeded() bool { return false }
