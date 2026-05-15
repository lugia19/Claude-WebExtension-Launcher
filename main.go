package main

import (
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/selfupdate"
	"claude-webext-patcher/utils"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var launchClaudeInTerminal = false

func init() {
	debugPath := filepath.Join(utils.GetExecutableDir(), "debug_on")
	if _, err := os.Stat(debugPath); err == nil {
		launchClaudeInTerminal = true
	}
}

// Version is the current version of the application
const Version = "2.1.2"

func main() {
	// Parse command-line flags
	forceUpdate := flag.Bool("force-update", false, "Force update to the latest version even if it's not verified compatible")
	instanceName := flag.String("instance", "modified", "Instance name for separate data directory and lock")
	patcherMode := flag.Bool("patcher", false, "Run in elevated patcher mode (internal)")
	uninstall := flag.Bool("uninstall", false, "Remove the patched Claude installation")
	uninstallElevated := flag.Bool("uninstall-elevated", false, "Run elevated uninstall (internal)")
	flag.Parse()

	fmt.Printf("Claude_WebExtension_Launcher version: %s\n", Version)
	// Set version for selfupdate module
	selfupdate.CurrentVersion = Version

	// Set embedded FS for patcher module
	patcher.EmbeddedFS = EmbeddedFS

	// Patcher mode: do admin work and exit (Windows only)
	if *patcherMode {
		os.Exit(runPatcherMode(*forceUpdate))
	}

	// Elevated uninstall mode (Windows internal)
	if *uninstallElevated {
		os.Exit(runUninstallElevated())
	}

	// Uninstall mode: remove the patched installation
	if *uninstall {
		os.Exit(runUninstall())
	}

	// Handle update completion first
	selfupdate.FinishUpdateIfNeeded()

	// Platform-specific setup before the main flow
	if err := prepareAdminContext(); err != nil {
		fmt.Printf("Failed to prepare admin context: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Claude WebExtension Launcher starting...")
	fmt.Printf("Version: %s\n", Version)

	// Check for self-updates
	if err := selfupdate.CheckAndUpdate(); err != nil {
		fmt.Printf("Update check failed: %v\n", err)
		// Continue anyway
	}

	// Ensure Claude is patched and extensions are up-to-date.
	// On Windows this may invoke an elevated patcher subprocess via UAC.
	// On macOS this runs in-process.
	if err := ensureClaudeReady(*forceUpdate); err != nil {
		if _, statErr := os.Stat(claudeExecutablePath()); statErr == nil {
			fmt.Printf("Warning: %v\n", err)
			fmt.Println("Continuing with existing installation...")
		} else {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	}

	// Release any platform-specific privileges before launching Claude
	releaseAdminContext()

	// Check for official Claude MSIX installation (Windows only)
	checkMSIXAndPrompt(*instanceName)

	// Clear caches that interfere with extension loading and updates
	claudeDataDir := claudeUserDataDir(*instanceName)
	if claudeDataDir != "" {
		cacheDirs := []string{"Service Worker", "WebStorage", "Cache", "Code Cache"}
		fmt.Printf("Clearing cache folders:\n")
		for _, dir := range cacheDirs {
			p := filepath.Join(claudeDataDir, dir)
			fmt.Printf("  %s\n", p)
			os.RemoveAll(p)
		}
		fmt.Println("Cache cleared successfully")
	}

	// Launch Claude
	fmt.Println("Launching Claude.")
	claudePath := claudeExecutablePath()
	instanceArg := fmt.Sprintf("--instance=%s", *instanceName)

	if launchClaudeInTerminal {
		// In developer mode, run Claude in the same terminal to see debug output
		cmd := exec.Command(claudePath, instanceArg)
		cmd.Dir = filepath.Dir(claudePath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Run()
	} else {
		// Launch detached
		cmd := exec.Command(claudePath, instanceArg)
		cmd.Dir = filepath.Dir(claudePath)
		cmd.Start()
	}
}
