package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

const executableName = "Claude_WebExtension_Launcher"

func selectAsset(assets []releaseAsset) (string, string, error) {
	suffix := fmt.Sprintf("-linux-%s.zip", runtime.GOARCH)
	fmt.Printf("Looking for Linux release (architecture: %s)...\n", runtime.GOARCH)

	for _, asset := range assets {
		if strings.HasSuffix(asset.Name, suffix) {
			fmt.Printf("Found release: %s\n", asset.Name)
			return asset.DownloadURL, asset.Name, nil
		}
	}

	fmt.Println("No release found for platform: linux")
	return "", "", fmt.Errorf("no compatible release file found for linux/%s", runtime.GOARCH)
}

// installUpdate swaps the new binary over the running one and re-execs it. Renaming
// over a running executable is safe on Linux: the old process keeps its open inode.
func installUpdate(tempDir, tempZip string) error {
	defer os.Remove(tempZip)
	defer os.RemoveAll(tempDir)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating current executable: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}

	data, err := os.ReadFile(filepath.Join(tempDir, executableName))
	if err != nil {
		return fmt.Errorf("update package has no %s: %v", executableName, err)
	}

	// Stage next to the target so the rename stays on one filesystem. Zip extraction
	// drops the exec bit, so set it explicitly.
	staged := exePath + ".new"
	if err := os.WriteFile(staged, data, 0755); err != nil {
		return fmt.Errorf("writing new executable: %v", err)
	}
	if err := os.Chmod(staged, 0755); err != nil {
		os.Remove(staged)
		return fmt.Errorf("making new executable runnable: %v", err)
	}
	if err := os.Rename(staged, exePath); err != nil {
		os.Remove(staged)
		return fmt.Errorf("replacing executable: %v", err)
	}

	os.Remove(tempZip)
	os.RemoveAll(tempDir)

	fmt.Println("Update installed, restarting...")
	args := append([]string{exePath}, os.Args[1:]...)
	if err := syscall.Exec(exePath, args, os.Environ()); err != nil {
		return fmt.Errorf("restarting updated launcher: %v", err)
	}
	return nil
}
