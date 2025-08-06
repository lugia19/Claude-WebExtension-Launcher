package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/selfupdate"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
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

	// Launch Claude
	fmt.Println("Launching Claude.")
	cmd := exec.Command(filepath.Join(patcher.AppFolder, "claude.exe"))
	cmd.Start()
}
