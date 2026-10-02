package main

import (
	"claude-webext-patcher/patcher"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// platformSetup has nothing to do on macOS (installedFrom: see main_windows.go).
func platformSetup(installedFrom string) {}

func claudeUserDataDir(instance string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Claude-"+instance)
}

func claudeExecutablePath() string {
	return filepath.Join(claudeApp(), "Contents", "MacOS", "Claude")
}

// claudeApp is the patched app bundle that claudeExecutablePath is inside.
func claudeApp() string {
	return filepath.Join(patcher.AppFolder, "Claude.app")
}

// startClaude opens Claude.app through LaunchServices rather than starting its binary
// as our child. A child counts as part of the launcher for privacy permissions
// (microphone, screen recording...), so Claude's requests were attributed to the
// launcher, which has usually exited by then, and Claude never showed up in System
// Settings' lists. -n starts a new copy even when another instance is running; env
// (dev mode's variables) has to go through --env, as the app doesn't inherit ours.
func startClaude(cmd *exec.Cmd, env []string) error {
	args := []string{"-n", "-a", claudeApp()}
	for _, kv := range env {
		args = append(args, "--env", kv)
	}
	args = append(append(args, "--args"), cmd.Args[1:]...)
	if output, err := exec.Command("/usr/bin/open", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("opening %s: %v\n%s", claudeApp(), err, strings.TrimSpace(string(output)))
	}
	return nil
}

// The sandbox (AppArmor) step is Linux-only, the Cowork service Windows-only.
func sandboxNeeded() bool   { return false }
func installSandbox() error { return nil }
func coworkNeeded() bool    { return false }
