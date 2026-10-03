//go:build windows

package selfupdate

import (
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const executableName = "Claude_WebExtension_Launcher.exe"

// finishUpdateIfNeeded completes an update staged by an older launcher (4.2.2 and
// before), which wrote the new version as a .new.exe next to itself and restarted
// into it: move it into place and restart from there. The old exe is moved aside
// rather than deleted, which works even while another launcher still runs from it.
// If that fails anyway, carry on from here this time: exiting would only look like
// the launcher not starting.
func finishUpdateIfNeeded(exePath string) {
	if strings.HasSuffix(filepath.Base(exePath), ".new.exe") {
		originalExe := strings.TrimSuffix(exePath, ".new.exe") + ".exe"
		if _, err := utils.ReplaceFile(exePath, originalExe, 0755); err != nil {
			fmt.Printf("Couldn't finish the launcher update: %v\n", err)
			return
		}
		err := restart(originalExe)
		fmt.Printf("Couldn't restart into the updated launcher: %v\n", err)
		return
	}

	// Clean up a .new.exe an older launcher staged. (The .old ReplaceFile can leave
	// behind is cleanupInstall's.)
	os.Remove(strings.TrimSuffix(exePath, ".exe") + ".new.exe")
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
	if _, err := os.Stat(filepath.Join(tempDir, executableName)); err != nil {
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
	restore := func() error { return nil }

	for _, entry := range entries {
		if entry.IsDir() {
			continue // Skip directories for now (flat structure expected)
		}

		srcPath := filepath.Join(tempDir, entry.Name())

		// The main executable replaces this one: moved aside while it runs, then
		// restarted below.
		if entry.Name() == executableName {
			restore, err = utils.ReplaceFile(srcPath, exePath, 0755)
			if err != nil {
				os.Remove(tempZip)
				os.RemoveAll(tempDir)
				return fmt.Errorf("failed to install the new executable: %v", err)
			}
			fmt.Printf("Updated: %s\n", entry.Name())
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

	if err := restart(exePath); err != nil {
		// Put the current version back, or every later launch would hit the new one.
		if rerr := restore(); rerr != nil {
			return fmt.Errorf("failed to start updated executable: %v (and couldn't restore the current one: %v)", err, rerr)
		}
		return fmt.Errorf("failed to start updated executable (kept the current version): %v", err)
	}
	return nil
}

const updateLockName = `Local\ClaudeWebExtLauncher-SelfUpdate`

// restart starts exe with this process's arguments, so flags like --instance survive
// the restart, and exits. Returns only if exe couldn't start.
func restart(exe string) error {
	if err := exec.Command(exe, os.Args[1:]...).Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
