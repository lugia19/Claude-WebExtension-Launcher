package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func selectAsset(assets []releaseAsset) (string, string, error) {
	arch := strings.ToLower(runtime.GOARCH) // "amd64" or "arm64"
	archSpecificSuffix := fmt.Sprintf("-macos-%s", arch)

	fmt.Printf("Looking for macOS release (architecture: %s)...\n", arch)

	// First try: architecture-specific (e.g., "-macos-arm64")
	for _, asset := range assets {
		if strings.Contains(asset.Name, archSpecificSuffix) && strings.HasSuffix(asset.Name, ".zip") {
			fmt.Printf("Found architecture-specific release: %s\n", asset.Name)
			return asset.DownloadURL, asset.Name, nil
		}
	}

	// Second try: generic macOS (e.g., "-macos")
	for _, asset := range assets {
		if strings.Contains(asset.Name, "-macos") && strings.HasSuffix(asset.Name, ".zip") {
			fmt.Printf("Found generic macOS release: %s\n", asset.Name)
			return asset.DownloadURL, asset.Name, nil
		}
	}

	fmt.Println("No release found for platform: darwin")
	return "", "", fmt.Errorf("no compatible release file found for darwin")
}

func installUpdate(tempDir, tempZip string) error {
	// macOS - download to Downloads folder, avoiding collisions only if needed
	homeDir, _ := os.UserHomeDir()
	exePath, _ := os.Executable()
	currentAppPath := filepath.Dir(filepath.Dir(filepath.Dir(exePath)))
	appName := "Claude_WebExtension_Launcher.app"
	newAppPath := filepath.Join(tempDir, appName)

	// Start with the original name
	baseAppName := "Claude_WebExtension_Launcher"
	downloadPath := filepath.Join(homeDir, "Downloads", baseAppName+".app")

	// Check if we need to avoid a collision
	if _, err := os.Stat(downloadPath); err == nil {
		// Something exists at this path - is it us?
		if downloadPath == currentAppPath {
			// We're running from Downloads! Need a different name
			fmt.Println("Running from Downloads folder - using alternative name...")

			// Try numbered versions until we find an available one
			for i := 1; i <= 10; i++ {
				if i == 1 {
					downloadPath = filepath.Join(homeDir, "Downloads", baseAppName+"_new.app")
				} else {
					downloadPath = filepath.Join(homeDir, "Downloads", fmt.Sprintf("%s_new_%d.app", baseAppName, i))
				}

				if _, err := os.Stat(downloadPath); os.IsNotExist(err) {
					break // Found an available name
				}
			}
		} else {
			// There's an old download there, but it's not us - just replace it
			os.RemoveAll(downloadPath)
		}
	}
	// else: nothing at that path, we can use the original name

	// Extract just the app name for display
	downloadedAppName := filepath.Base(downloadPath)

	// Move/copy the new app to Downloads
	if err := exec.Command("cp", "-R", newAppPath, downloadPath).Run(); err != nil {
		// Fallback to basic copy
		os.Rename(newAppPath, downloadPath)
	}

	// Make the executable actually executable
	execPath := filepath.Join(downloadPath, "Contents", "MacOS", "Claude_WebExtension_Launcher")
	if err := os.Chmod(execPath, 0755); err != nil {
		fmt.Printf("Warning: Failed to set executable permissions: %v\n", err)
		exec.Command("chmod", "+x", execPath).Run()
	}

	// Remove quarantine attribute
	exec.Command("xattr", "-cr", downloadPath).Run()

	// Clean up temp files
	os.Remove(tempZip)
	os.RemoveAll(tempDir)

	// Show in Finder
	exec.Command("open", "-R", downloadPath).Run()

	how := fmt.Sprintf("Drag '%s' from Downloads (now open in Finder) to Applications, "+
		"replacing the old one, then launch it again.", strings.TrimSuffix(downloadedAppName, ".app"))
	fmt.Println("Launcher update downloaded. " + how)
	Notify("Launcher update downloaded", how)

	os.Exit(0)
	return nil
}

// lockUpdate is a no-op on macOS: the update is only downloaded to ~/Downloads for
// the user to install, and never replaces the running launcher.
func lockUpdate() (func(), bool) {
	return func() {}, true
}

// finishUpdateIfNeeded is a no-op on macOS: the new bundle is handed to the user.
func finishUpdateIfNeeded(exePath string) {}
