//go:build !windows

package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"time"
)

// releaseAdminContext is a no-op on non-Windows platforms.
func releaseAdminContext() {}

// ensureConsole is a no-op on non-Windows platforms, which have no GUI subsystem split.
func ensureConsole() {}

// claudeInstalled returns true if the Claude executable exists in the install directory.
func claudeInstalled() bool {
	_, err := os.Stat(claudeExecutablePath())
	return err == nil
}

const (
	// patchLockName serializes patching and extension updates across launchers
	// started together (e.g. several named instances), which share the staging,
	// asar-temp and web-extensions paths.
	patchLockName = "patch"
	// patchLockTimeout is generous: a real update downloads Claude (~175-250 MB).
	patchLockTimeout = 10 * time.Minute
)

// ensureClaudeReady runs patching and extension updates in-process on macOS and Linux.
func ensureClaudeReady(forceUpdate bool) error {
	lock, locked := utils.AcquirePatchLock(patchLockName, patchLockTimeout)
	if !locked {
		if claudeInstalled() {
			// The staging swap never leaves a half-written install, so launching
			// whatever is there is safe.
			fmt.Println("Warning: timed out waiting for another launcher to finish updating; launching existing installation.")
			return nil
		}
		// Nothing to launch yet (e.g. a slow first download in another launcher).
		// Never patch without the lock: keep waiting for it instead.
		fmt.Println("Waiting for another launcher to finish installing Claude...")
		if lock, locked = utils.AcquirePatchLock(patchLockName, 24*time.Hour); !locked {
			return fmt.Errorf("could not acquire the install lock")
		}
	}
	// No separate re-check needed: EnsurePatched compares against the version files,
	// so a launcher that waited here finds the work already done.
	defer lock.Release()

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
func runPatcherMode(forceUpdate, debug bool, packagePath, packageVersion string) int {
	fmt.Println("--patcher is not supported on this platform")
	return 1
}
