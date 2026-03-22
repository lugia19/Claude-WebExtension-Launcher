package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/selfupdate"
	"claude-webext-patcher/utils"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var launchClaudeInTerminal = false

func init() {
	debugPath := filepath.Join(utils.GetExecutableDir(), "debug_on")
	if _, err := os.Stat(debugPath); err == nil {
		launchClaudeInTerminal = true
	}
}

// Version is the current version of the application
const Version = "2.0.1"

func main() {
	// Parse command-line flags
	forceUpdate := flag.Bool("force-update", false, "Force update to the latest version even if it's not verified compatible")
	flag.Parse()

	fmt.Printf("Claude_WebExtension_Launcher version: %s\n", Version)
	// Set version for selfupdate module
	selfupdate.CurrentVersion = Version

	// Set embedded FS for patcher module
	patcher.EmbeddedFS = EmbeddedFS

	// Handle update completion first
	selfupdate.FinishUpdateIfNeeded()

	// On Windows, ensure we're running as admin (needed for WindowsApps folder setup).
	// We use programmatic elevation instead of a manifest so the self-update flow works.
	if runtime.GOOS == "windows" && !utils.IsAdmin() {
		fmt.Println("Requesting administrator privileges...")
		if err := utils.RelaunchAsAdmin(); err != nil {
			fmt.Printf("Failed to elevate: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Take ownership of WindowsApps early so all subsequent operations can access it
	if runtime.GOOS == "windows" {
		if err := patcher.TakeWindowsAppsOwnership(); err != nil {
			fmt.Printf("Warning: failed to take WindowsApps ownership: %v\n", err)
		}
	}

	// On macOS, if not running in terminal, relaunch in Terminal.app
	if runtime.GOOS == "darwin" && os.Getenv("TERM") == "" {
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

	fmt.Println("Claude WebExtension Launcher starting...")
	fmt.Printf("Version: %s\n", Version)

	// Clean up old installation files next to the executable (from before the move to WindowsApps)
	if runtime.GOOS == "windows" {
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
	}

	// Check for self-updates
	if err := selfupdate.CheckAndUpdate(); err != nil {
		fmt.Printf("Update check failed: %v\n", err)
		// Continue anyway
	}

	// Ensure app is installed, updated, and patched
	if err := patcher.EnsurePatched(*forceUpdate); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Update extensions
	if err := extensions.UpdateAll(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	//Clear service worker cache
	var serviceWorkerPath, webStoragePath string

	switch runtime.GOOS {
	case "windows":
		serviceWorkerPath = filepath.Join(os.Getenv("APPDATA"), "Claude", "Service Worker")
		webStoragePath = filepath.Join(os.Getenv("APPDATA"), "Claude", "WebStorage")
	case "darwin":
		home, _ := os.UserHomeDir()
		appSupport := filepath.Join(home, "Library", "Application Support", "Claude")
		serviceWorkerPath = filepath.Join(appSupport, "Service Worker")
		webStoragePath = filepath.Join(appSupport, "WebStorage")
	}

	if serviceWorkerPath != "" {
		fmt.Printf("Clearing cache folders:\n")
		fmt.Printf("  Service Worker: %s\n", serviceWorkerPath)
		fmt.Printf("  Web Storage: %s\n", webStoragePath)

		os.RemoveAll(serviceWorkerPath)
		os.RemoveAll(webStoragePath)
		fmt.Println("Cache cleared successfully")
	}

	// Release WindowsApps permissions before launching Claude
	if runtime.GOOS == "windows" {
		patcher.ReleaseWindowsAppsOwnership()
	}

	// Launch Claude
	fmt.Println("Launching Claude.")
	var claudePath string
	switch runtime.GOOS {
	case "windows":
		claudePath = filepath.Join(patcher.AppFolder, "claude.exe")
	case "darwin":
		// macOS app bundle structure
		claudePath = filepath.Join(patcher.AppFolder, "Claude.app", "Contents", "MacOS", "Claude")
	default:
		// Linux and other Unix-like systems
		claudePath = filepath.Join(patcher.AppFolder, "claude")
	}

	if launchClaudeInTerminal {
		// In developer mode, run Claude in the same terminal to see debug output
		cmd := exec.Command(claudePath)
		cmd.Dir = filepath.Dir(claudePath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Run()
	} else {
		// Launch detached
		cmd := exec.Command(claudePath)
		cmd.Dir = filepath.Dir(claudePath)
		cmd.Start()
	}
}
