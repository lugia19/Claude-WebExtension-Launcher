package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/selfupdate"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const launchClaudeInTerminal = false

// Version is the current version of the application
const Version = "1.1.0"

func main() {
	// Set version for selfupdate module
	selfupdate.CurrentVersion = Version

	// Set embedded FS for patcher module
	patcher.EmbeddedFS = EmbeddedFS

	// Handle update completion first
	selfupdate.FinishUpdateIfNeeded()

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

	// Check for self-updates
	if err := selfupdate.CheckAndUpdate(); err != nil {
		fmt.Printf("Update check failed: %v\n", err)
		// Continue anyway
	}

	// Ensure app is installed, updated, and patched
	if err := patcher.EnsurePatched(); err != nil {
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

	// Launch Claude
	fmt.Println("Launching Claude...")

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		claudePath := filepath.Join(patcher.AppFolder, "claude.exe")
		cmd = exec.Command(claudePath)
	case "darwin":
		// Use LaunchServices for GUI app launch on macOS
		claudeApp := filepath.Join(patcher.AppFolder, "Claude.app")
		fmt.Println("Launching Claude via LaunchServices (open -n)...")
		cmd = exec.Command("open", "-n", claudeApp)
	default:
		// Linux and other Unix-like systems
		claudePath := filepath.Join(patcher.AppFolder, "claude")
		cmd = exec.Command(claudePath)
	}

	if launchClaudeInTerminal && runtime.GOOS != "darwin" {
		// In developer mode (non-macOS), run Claude in the same terminal to see debug output
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		_ = cmd.Run()
	} else {
		// Normal mode - launch detached
		if err := cmd.Start(); err != nil {
			fmt.Printf("Failed to launch Claude: %v\n", err)
		} else {
			fmt.Println("Claude launch command issued.")
		}
	}
}
