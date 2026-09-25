//go:build windows

package selfupdate

import (
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const executableName = "Claude_WebExtension_Launcher.exe"

func finishUpdateIfNeeded(exePath string) {
	exeName := filepath.Base(exePath)

	if strings.HasSuffix(exeName, ".new.exe") {
		originalExe := strings.TrimSuffix(exePath, ".new.exe") + ".exe"

		// Wait a bit for the original to fully exit
		time.Sleep(500 * time.Millisecond)

		// Try to delete with retries
		for i := 0; i < 5; i++ {
			if err := os.Remove(originalExe); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Copy ourselves to the original name
		input, _ := os.ReadFile(exePath)
		if err := os.WriteFile(originalExe, input, 0755); err != nil {
			fmt.Printf("Failed to write update: %v\n", err)
			os.Exit(1)
		}

		// Launch the original in new console window, forwarding our own arguments so
		// flags like --instance survive the restart instead of silently falling back
		// to the default instance.
		// Need to quote the path for cmd /c start to handle spaces
		startArgs := append([]string{"/c", "start", "Claude Desktop (Extended)", originalExe}, os.Args[1:]...)
		cmd := utils.Command("cmd", startArgs...)
		cmd.Start()

		os.Exit(0)
	}

	// Clean up any temporary update files
	newExePath := strings.TrimSuffix(exePath, ".exe") + ".new.exe"
	os.Remove(newExePath)
}

func selectAsset(assets []releaseAsset) (string, string, error) {
	for _, asset := range assets {
		if strings.Contains(asset.Name, "-windows") && strings.HasSuffix(asset.Name, ".zip") {
			return asset.DownloadURL, asset.Name, nil
		}
	}
	fmt.Println("No release found for platform: windows")
	return "", "", fmt.Errorf("no compatible release file found for windows")
}

func installUpdate(tempDir, tempZip string) error {
	// First, make sure the executable exists
	newExePath := filepath.Join(tempDir, executableName)
	if _, err := os.Stat(newExePath); err != nil {
		os.Remove(tempZip)
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to find executable in update: %v", err)
	}

	// Copy ALL files from the update package to the application directory
	// This ensures any helper scripts, resources, etc. are also updated
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		os.Remove(tempZip)
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to read update directory: %v", err)
	}

	exePath, _ := os.Executable()
	appDir := filepath.Dir(exePath)

	for _, entry := range entries {
		if entry.IsDir() {
			continue // Skip directories for now (flat structure expected)
		}

		srcPath := filepath.Join(tempDir, entry.Name())

		// Special handling for the main executable - use .new suffix
		if entry.Name() == executableName {
			dstPath := filepath.Join(appDir, strings.TrimSuffix(entry.Name(), ".exe")+".new.exe")
			srcData, err := os.ReadFile(srcPath)
			if err != nil {
				os.Remove(tempZip)
				os.RemoveAll(tempDir)
				return fmt.Errorf("failed to read executable: %v", err)
			}
			if err := os.WriteFile(dstPath, srcData, 0755); err != nil {
				os.Remove(tempZip)
				os.RemoveAll(tempDir)
				return fmt.Errorf("failed to write new executable: %v", err)
			}
			fmt.Printf("Staged update: %s\n", entry.Name())
		} else {
			// For all other files, copy them directly
			dstPath := filepath.Join(appDir, entry.Name())
			srcData, err := os.ReadFile(srcPath)
			if err != nil {
				fmt.Printf("Warning: Failed to read %s: %v\n", entry.Name(), err)
				continue
			}
			if err := os.WriteFile(dstPath, srcData, 0755); err != nil {
				fmt.Printf("Warning: Failed to update %s: %v\n", entry.Name(), err)
			} else {
				fmt.Printf("Updated: %s\n", entry.Name())
			}
		}
	}

	// Clean up temp files before restarting
	os.Remove(tempZip)
	os.RemoveAll(tempDir)

	fmt.Println("Restarting to complete update...")

	// Launch the new exe, forwarding our own arguments so flags like --instance
	// survive the update restart.
	newExeName := filepath.Join(appDir, strings.TrimSuffix(executableName, ".exe")+".new.exe")
	cmd := exec.Command(newExeName, os.Args[1:]...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start updated executable: %v", err)
	}
	// Exit to let it take over
	os.Exit(0)
	return nil
}

// lockUpdate is a no-op on Windows, which swaps via a .new.exe on restart.
func lockUpdate() (func(), bool) {
	return func() {}, true
}
