//go:build !windows

package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"fmt"
	"os"
)

// releaseAdminContext is a no-op on non-Windows platforms.
func releaseAdminContext() {}

// claudeInstalled returns true if the Claude executable exists in the install directory.
func claudeInstalled() bool {
	_, err := os.Stat(claudeExecutablePath())
	return err == nil
}

// ensureClaudeReady runs patching and extension updates in-process on macOS and Linux.
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
func runPatcherMode(forceUpdate bool, debug bool) int {
	fmt.Println("--patcher is not supported on this platform")
	return 1
}
