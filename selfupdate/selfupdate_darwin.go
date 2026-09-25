package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"claude-webext-patcher/utils"
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
	newApp := filepath.Join(tempDir, "Claude_WebExtension_Launcher.app")
	if _, err := os.Stat(newApp); err != nil {
		return fmt.Errorf("the update has no Claude_WebExtension_Launcher.app: %w", err)
	}

	// The running app is the installed one (the launcher installs itself to
	// ~/Applications and runs from there): replace it in place and restart.
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exePath))) // <app>/Contents/MacOS/<exe>
	if !strings.HasSuffix(app, ".app") {
		return fmt.Errorf("not running from an app bundle (%s)", exePath)
	}
	if err := utils.InstallAppBundle(newApp, app); err != nil {
		return err
	}
	os.Remove(tempZip)
	os.RemoveAll(tempDir)

	bin := filepath.Join(app, "Contents", "MacOS", "Claude_WebExtension_Launcher")
	os.Chmod(bin, 0755)
	fmt.Println("Launcher updated, restarting...")
	err = syscall.Exec(bin, append([]string{bin}, os.Args[1:]...), os.Environ())
	return fmt.Errorf("restarting into the updated launcher: %w", err)
}

// lockUpdate takes a per-user lock for the update, so two launchers don't replace the
// app at the same time.
func lockUpdate() (func(), bool) {
	lock, ok := utils.AcquirePatchLock("selfupdate", 5*time.Minute)
	if !ok {
		return nil, false
	}
	return lock.Release, true
}

// finishUpdateIfNeeded has nothing to do on macOS: the update replaces the app bundle
// completely (utils.InstallAppBundle cleans up after itself).
func finishUpdateIfNeeded(exePath string) {}
