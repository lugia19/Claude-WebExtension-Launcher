package main

import (
	"claude-webext-patcher/patcher"
	"os"
	"os/exec"
	"path/filepath"
)

// platformSetup has nothing to do on macOS.
func platformSetup() {}

func claudeUserDataDir(instance string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Claude-"+instance)
}

func claudeExecutablePath() string {
	return filepath.Join(patcher.AppFolder, "Claude.app", "Contents", "MacOS", "Claude")
}

// detachFromTerminal is a no-op here; see main_linux.go.
func detachFromTerminal(cmd *exec.Cmd) {}

// The sandbox (AppArmor) step is Linux-only, the Cowork service Windows-only.
func sandboxNeeded() bool   { return false }
func installSandbox() error { return nil }
func coworkNeeded() bool    { return false }
