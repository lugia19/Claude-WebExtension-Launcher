//go:build windows

package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// patchLockName serializes the elevated patcher across concurrently-launched
	// instances (all run as the same user in one session, so Local\ is shared).
	patchLockName = `Local\ClaudeWebExtLauncher-Patch`
	// patchLockTimeout is generous: a real patch downloads the ~222 MB MSIX.
	patchLockTimeout = 5 * time.Minute
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
func runPatcherMode(forceUpdate bool, debug bool) int {
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
		if debug {
			fmt.Println("Press Enter to continue...")
			fmt.Scanln()
		}
	}

	if err := patcher.DeploySentinelExtension(); err != nil {
		fmt.Printf("Warning: sentinel extension deployment failed: %v\n", err)
		if debug {
			fmt.Println("Press Enter to continue...")
			fmt.Scanln()
		}
	}

	patcher.GrantUserReadAccess()

	if err := patcher.RegisterCoworkService(); err != nil {
		fmt.Printf("Warning: Cowork service registration failed: %v\n", err)
		if debug {
			fmt.Println("Press Enter to continue...")
			fmt.Scanln()
		}
	}

	patcher.ReleaseWindowsAppsOwnership()

	fmt.Println("Patching complete.")
	if debug {
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
	}
	return 0
}

// ensureClaudeReady checks whether admin work is needed and, if so, invokes
// the launcher in elevated patcher mode via UAC.
//
// When a patch is actually needed, it is serialized behind a cross-process lock so
// that launching many instances at once (e.g. a 10-instance batch template) can't
// spawn 10 elevated patchers that race and corrupt the shared install. The first
// instance patches (one UAC prompt); the rest block, then re-check and skip.
func ensureClaudeReady(forceUpdate bool) error {
	// Fast path: if nothing needs patching, don't take the lock — let the common
	// case stay fully concurrent (this check is read-only + a version query).
	if !checkNeedsAdmin(forceUpdate) {
		fmt.Println("Claude is up to date, no admin work needed.")
		return nil
	}

	// A patch is needed: serialize it.
	lock, locked := utils.AcquirePatchLock(patchLockName, patchLockTimeout)
	if locked {
		defer lock.Release()
		// Re-check under the lock — another instance may have just finished patching.
		if !checkNeedsAdmin(forceUpdate) {
			fmt.Println("Claude was patched by another instance; continuing.")
			return nil
		}
	} else if claudeInstalled() {
		// Waited past the timeout for another instance. The atomic staging-swap
		// guarantees the install is never left stock/half-written, so launching
		// whatever is present is safe.
		fmt.Println("Warning: timed out waiting for another instance to finish patching; launching existing installation.")
		return nil
	}
	// If we couldn't lock AND there's no install yet, fall through and patch anyway.

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	args := "--patcher"
	if forceUpdate {
		args += " --force-update"
	}
	if launchClaudeInTerminal {
		args += " --debug"
	}

	// Download Claude here, unelevated, so the status window can show progress. The
	// elevated patcher verifies the package's signature before using it, and downloads
	// it itself if this fails or the package doesn't check out.
	pkg, pkgVersion, err := patcher.PrefetchWindowsPackage(forceUpdate)
	if err != nil {
		fmt.Printf("Warning: could not download Claude in advance (%v); the patcher will download it.\n", err)
	} else if pkg != "" {
		defer os.Remove(pkg)
		args += fmt.Sprintf(` --package="%s" --package-version=%s`, pkg, pkgVersion)
	}

	step("Waiting for administrator permission...")
	fmt.Println("Administrator privileges required for patching...")
	exitCode, err := utils.RunElevatedAndWait(exe, args)
	step("Checking for Claude updates...")
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

	// If the Cowork service is missing, run the elevated patcher (which registers it).
	// Checked before any network call so it works offline too.
	if !patcher.CoworkServiceExists() {
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

	// Check if a newer Claude version is available
	newestVersion, _, err := patcher.GetLatestVersion()
	if err != nil {
		// Can't reach update server — assume current install is fine
		return false
	}
	if currentVersion != newestVersion {
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

// detachFromTerminal is a no-op here; see main_linux.go.
func detachFromTerminal(cmd *exec.Cmd) {}

// ensureConsole attaches or opens a console for output (the build is GUI-subsystem).
func ensureConsole() {
	utils.EnsureConsole()
}
