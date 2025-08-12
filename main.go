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
)

func main() {
	// On macOS, if not running in terminal, relaunch in Terminal.app
	if runtime.GOOS == "darwin" && os.Getenv("TERM") == "" {
		executable, _ := os.Executable()
		script := fmt.Sprintf(`tell application "Terminal"
			do script "%s"
			activate
		end tell`, executable)
		cmd := exec.Command("osascript", "-e", script)
		cmd.Start()
		os.Exit(0)
	}

	// Handle update completion first
	selfupdate.FinishUpdateIfNeeded()

	fmt.Println("Claude Manager starting...")

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
	serviceWorkerPath := filepath.Join(os.Getenv("APPDATA"), "Claude", "Service Worker")
	webStoragePath := filepath.Join(os.Getenv("APPDATA"), "Claude", "WebStorage")

	os.RemoveAll(serviceWorkerPath)
	os.RemoveAll(webStoragePath)

	fmt.Println("Cleared cache folders")

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
	cmd := exec.Command(claudePath)
	cmd.Start()
}
