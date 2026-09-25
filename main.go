package main

import (
	"claude-webext-patcher/gui"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/selfupdate"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var launchClaudeInTerminal = false

// guiMode is true while the launcher runs behind the status window, so platform code
// knows not to relaunch itself in a terminal.
var guiMode = false

// Version is the current version of the application
const Version = "3.3.3"

// defaultInstanceName is the instance used when --instance is not given. Only this
// instance shares Cowork/Code sessions with the official install; named instances stay
// isolated (see SetupSessionSharing / RepairSessionSharing).
const defaultInstanceName = "modified"

func main() {
	// Parse command-line flags
	forceUpdate := flag.Bool("force-update", false, "Re-download and re-patch Claude even if already up to date")
	instanceName := flag.String("instance", defaultInstanceName, "Instance name for separate data directory and lock")
	patcherMode := flag.Bool("patcher", false, "Run in elevated patcher mode (internal)")
	debug := flag.Bool("debug", false, "Keep console windows open and launch Claude attached to terminal")
	packagePath := flag.String("package", "", "Claude package downloaded by the unelevated launcher (internal)")
	packageVersion := flag.String("package-version", "", "Version of --package (internal)")
	flag.Parse()

	launchClaudeInTerminal = *debug
	noGUI := *debug || os.Getenv("CLAUDE_WEBEXT_NO_GUI") != ""

	// Windows builds are GUI-subsystem apps: give them a console only when output is
	// meant to be read there (the elevated patcher always shows its own).
	if noGUI || *patcherMode {
		ensureConsole()
	}

	fmt.Printf("Claude_WebExtension_Launcher version: %s\n", Version)
	// Set version for selfupdate module
	selfupdate.CurrentVersion = Version

	// Set embedded FS and debug flag for patcher module
	patcher.EmbeddedFS = EmbeddedFS
	patcher.Debug = *debug

	// Patcher mode: do admin work and exit (Windows only)
	if *patcherMode {
		os.Exit(runPatcherMode(*forceUpdate, *debug, *packagePath, *packageVersion))
	}

	// Show the status window unless the terminal is wanted: --debug keeps everything
	// in the terminal, and CLAUDE_WEBEXT_NO_GUI=1 is an escape hatch while this is new.
	if noGUI {
		if err := runLauncher(*forceUpdate, *instanceName); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	guiMode = true
	err := gui.Run("Claude WebExtension Launcher", func(s *gui.Status) error {
		ui = s
		patcher.DownloadProgress = s.DownloadProgress
		return runLauncher(*forceUpdate, *instanceName)
	})
	if err != nil {
		os.Exit(1)
	}
}

// runLauncher is the launcher's flow after flag parsing: update itself, make sure
// Claude is patched and extensions are current, then start Claude.
func runLauncher(forceUpdate bool, instanceName string) error {
	// Handle update completion first
	selfupdate.FinishUpdateIfNeeded()

	// Platform-specific setup before the main flow
	if err := prepareAdminContext(); err != nil {
		return fmt.Errorf("failed to prepare admin context: %v", err)
	}

	fmt.Println("Claude WebExtension Launcher starting...")
	fmt.Printf("Version: %s\n", Version)

	// Check for self-updates
	ui.Step("Checking for launcher updates...")
	if err := selfupdate.CheckAndUpdate(); err != nil {
		fmt.Printf("Update check failed: %v\n", err)
		// Continue anyway
	}

	// Ensure Claude is patched and extensions are up-to-date.
	// On Windows this may invoke an elevated patcher subprocess via UAC.
	// On macOS and Linux this runs in-process.
	ui.Step("Checking for Claude updates...")
	if err := ensureClaudeReady(forceUpdate); err != nil {
		if _, statErr := os.Stat(claudeExecutablePath()); statErr != nil {
			return err
		}
		fmt.Printf("Warning: %v\n", err)
		fmt.Println("Continuing with existing installation...")
	}

	// Release any platform-specific privileges before launching Claude
	releaseAdminContext()

	// Reconcile Cowork/Code session sharing before any uninstall prompt (Windows only):
	// repair a named instance that was wrongly pooled by an older build, then (only for the
	// default instance) share with the official install via junctions into a neutral store.
	RepairSessionSharing(instanceName)
	SetupSessionSharing(instanceName)

	// Check for official Claude MSIX installation (Windows only)
	checkMSIXAndPrompt(instanceName)

	// Clear caches that interfere with extension loading and updates
	claudeDataDir := claudeUserDataDir(instanceName)
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
	ui.Step("Launching Claude...")
	fmt.Println("Launching Claude.")
	claudePath := claudeExecutablePath()
	instanceArg := fmt.Sprintf("--instance=%s", instanceName)

	if launchClaudeInTerminal {
		// In developer mode, run Claude in the same terminal to see debug output
		cmd := exec.Command(claudePath, instanceArg)
		cmd.Dir = filepath.Dir(claudePath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Run()
		return nil
	}

	// Launch detached
	cmd := exec.Command(claudePath, instanceArg)
	cmd.Dir = filepath.Dir(claudePath)
	detachFromTerminal(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start Claude: %v", err)
	}
	return nil
}
