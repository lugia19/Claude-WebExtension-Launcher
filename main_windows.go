//go:build windows

package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// prepareAdminContext cleans up old installation files from the launcher directory.
// Unlike before Phase 2, this no longer self-elevates — elevation is handled
// on-demand by ensureClaudeReady when admin work is actually needed.
func prepareAdminContext() error {
	execDir := utils.GetExecutableDir()
	for _, oldDir := range []string{"app-latest", "web-extensions"} {
		oldPath := filepath.Join(execDir, oldDir)
		if _, err := os.Stat(oldPath); err == nil {
			fmt.Printf("Removing old %s from launcher directory...\n", oldDir)
			if err := os.RemoveAll(oldPath); err != nil {
				fmt.Printf("Warning: could not remove %s: %v\n", oldPath, err)
			}
		}
	}
	return nil
}

// releaseAdminContext is a no-op — ownership is managed by the patcher subprocess.
func releaseAdminContext() {}

func claudeUserDataDir(instance string) string {
	return filepath.Join(os.Getenv("APPDATA"), "Claude-"+instance)
}

func claudeExecutablePath() string {
	return filepath.Join(patcher.AppFolder, "claude.exe")
}

// runPatcherMode runs the elevated patcher code path. Called when the launcher
// is re-invoked with --patcher via UAC.
func runPatcherMode(forceUpdate bool) int {
	fmt.Println("Running in elevated patcher mode...")

	if err := patcher.TakeWindowsAppsOwnership(); err != nil {
		fmt.Printf("Failed to take WindowsApps ownership: %v\n", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		return 1
	}

	if err := patcher.EnsurePatched(forceUpdate); err != nil {
		fmt.Printf("Patching failed: %v\n", err)
		patcher.ReleaseWindowsAppsOwnership()
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		return 1
	}

	if err := extensions.UpdateAll(); err != nil {
		fmt.Printf("Warning: extension update failed: %v\n", err)
	}

	if err := patcher.DeploySentinelExtension(); err != nil {
		fmt.Printf("Warning: sentinel extension deployment failed: %v\n", err)
	}

	patcher.GrantUserReadAccess()
	patcher.ReleaseWindowsAppsOwnership()

	fmt.Println("Patching complete.")
	return 0
}

// ensureClaudeReady checks whether admin work is needed and, if so, invokes
// the launcher in elevated patcher mode via UAC.
func ensureClaudeReady(forceUpdate bool) error {
	needsAdmin := checkNeedsAdmin(forceUpdate)

	if !needsAdmin {
		fmt.Println("Claude is up to date, no admin work needed.")
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	args := "--patcher"
	if forceUpdate {
		args += " --force-update"
	}

	fmt.Println("Administrator privileges required for patching...")
	exitCode, err := utils.RunElevatedAndWait(exe, args)
	if err != nil {
		// UAC denied or ShellExecuteEx failed
		if claudeInstalled() {
			fmt.Printf("Warning: elevation failed (%v), launching existing installation.\n", err)
			return nil
		}
		return fmt.Errorf("elevation failed and no existing installation: %v", err)
	}

	if exitCode != 0 {
		if claudeInstalled() {
			fmt.Printf("Warning: patcher exited with code %d, launching existing installation.\n", exitCode)
			return nil
		}
		return fmt.Errorf("patcher failed (exit code %d) and no existing installation", exitCode)
	}

	return nil
}

// checkNeedsAdmin determines whether the elevated patcher needs to run.
func checkNeedsAdmin(forceUpdate bool) bool {
	if forceUpdate {
		return true
	}

	installDir := patcher.InstallBaseDir()

	// Check if Claude is installed
	claudeVersionFile := filepath.Join(installDir, "claude-version.txt")
	currentVersionData, err := os.ReadFile(claudeVersionFile)
	if err != nil {
		return true
	}
	currentVersion := strings.TrimSpace(string(currentVersionData))

	// Check patch version
	patchVersionFile := filepath.Join(installDir, "patch-version.txt")
	patchData, err := os.ReadFile(patchVersionFile)
	if err != nil || strings.TrimSpace(string(patchData)) != patcher.PatchVersion {
		return true
	}

	// Check if a newer verified Claude version is available
	newestVersion, _, err := patcher.GetLatestVersion()
	if err != nil {
		// Can't reach update server — assume current install is fine
		return false
	}
	if currentVersion != newestVersion && patcher.IsVersionVerified(newestVersion) {
		return true
	}

	// Check if extensions need updating
	if extensions.NeedsUpdate() {
		return true
	}

	return false
}

// claudeInstalled returns true if claude.exe exists in the install directory.
func claudeInstalled() bool {
	_, err := os.Stat(claudeExecutablePath())
	return err == nil
}
