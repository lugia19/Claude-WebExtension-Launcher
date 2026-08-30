//go:build linux

package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// executableNameLinux is the name of the launcher binary inside the Linux
// release zip (build-all.sh / build-all.ps1).
const executableNameLinux = "Claude_WebExtension_Launcher"

// selectAsset picks the architecture-matched Linux release asset first (e.g.
// -linux-arm64), falling back to a generic -linux asset. This prevents an arm64
// machine from ever downloading the amd64 build.
func selectAsset(assets []releaseAsset) (string, string, error) {
	return linuxSelectAsset(assets, runtime.GOARCH)
}

// linuxSelectAsset is split out for unit testing both architectures.
func linuxSelectAsset(assets []releaseAsset, arch string) (string, string, error) {
	archSuffix := fmt.Sprintf("-linux-%s", arch)
	fmt.Printf("Looking for Linux release (architecture: %s)...\n", arch)

	for _, asset := range assets {
		if strings.Contains(asset.Name, archSuffix) && strings.HasSuffix(asset.Name, ".zip") {
			fmt.Printf("Found architecture-specific release: %s\n", asset.Name)
			return asset.DownloadURL, asset.Name, nil
		}
	}

	// Fall back to a generic -linux asset (e.g. an older single-archive naming).
	for _, asset := range assets {
		if strings.Contains(asset.Name, "-linux") && strings.HasSuffix(asset.Name, ".zip") {
			fmt.Printf("Found generic Linux release: %s\n", asset.Name)
			return asset.DownloadURL, asset.Name, nil
		}
	}

	fmt.Println("No release found for platform: linux")
	return "", "", fmt.Errorf("no compatible release file found for linux")
}

// installUpdate replaces the running launcher in place: the new binary is
// staged next to it and atomically renamed over the old one (safe on Linux for
// a currently-executing file), then the process restarts with the same
// arguments. No macOS .app/xattr/Finder logic is ever reached on Linux.
func installUpdate(tempDir, tempZip string) error {
	exePath, err := os.Executable()
	if err != nil {
		os.Remove(tempZip)
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to resolve current executable: %v", err)
	}
	appDir := filepath.Dir(exePath)

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		os.Remove(tempZip)
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to read update package: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		srcPath := filepath.Join(tempDir, entry.Name())

		if entry.Name() == executableNameLinux {
			staged := exePath + ".new"
			if err := copyFileExec(srcPath, staged); err != nil {
				os.Remove(staged)
				os.Remove(tempZip)
				os.RemoveAll(tempDir)
				return fmt.Errorf("failed to stage new executable: %v", err)
			}
			if err := os.Rename(staged, exePath); err != nil {
				os.Remove(staged)
				os.Remove(tempZip)
				os.RemoveAll(tempDir)
				return fmt.Errorf("failed to install new executable: %v", err)
			}
			fmt.Printf("Updated: %s\n", entry.Name())
		} else {
			// Any other bundled file (e.g. the .desktop template) is refreshed
			// alongside the binary.
			dstPath := filepath.Join(appDir, entry.Name())
			if err := copyFileExec(srcPath, dstPath); err != nil {
				fmt.Printf("Warning: failed to update %s: %v\n", entry.Name(), err)
			} else {
				fmt.Printf("Updated: %s\n", entry.Name())
			}
		}
	}

	os.Remove(tempZip)
	os.RemoveAll(tempDir)

	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("updated executable not found at %s: %v", exePath, err)
	}

	fmt.Println("Restarting to complete update...")
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Dir = appDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start updated executable: %v", err)
	}
	os.Exit(0)
	return nil
}

// copyFileExec writes src onto dst with an executable mode.
func copyFileExec(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0755)
}
