package selfupdate

import (
	"debug/elf"
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

	// Stage next to the target so the renames stay on one filesystem. Zip extraction
	// drops the exec bit, so set it explicitly.
	staged := exePath + ".new"
	if err := os.WriteFile(staged, data, 0755); err != nil {
		return fmt.Errorf("writing new executable: %v", err)
	}
	defer os.Remove(staged)
	if err := os.Chmod(staged, 0755); err != nil {
		return fmt.Errorf("making new executable runnable: %v", err)
	}
	if err := checkLinuxExecutable(staged); err != nil {
		return fmt.Errorf("downloaded update is not a valid launcher: %v", err)
	}

	// Keep the current binary until the new one has actually started, so a failed
	// exec can be rolled back instead of leaving no working launcher.
	backup := exePath + ".old"
	os.Remove(backup)
	if err := os.Rename(exePath, backup); err != nil {
		return fmt.Errorf("moving current executable aside: %v", err)
	}
	if err := os.Rename(staged, exePath); err != nil {
		os.Rename(backup, exePath)
		return fmt.Errorf("replacing executable: %v", err)
	}

	os.Remove(tempZip)
	os.RemoveAll(tempDir)

	fmt.Println("Update installed, restarting...")
	args := append([]string{exePath}, os.Args[1:]...)
	err = syscall.Exec(exePath, args, os.Environ())

	// Exec only returns on failure: put the old launcher back and carry on with it.
	os.Remove(exePath)
	os.Rename(backup, exePath)
	return fmt.Errorf("restarting updated launcher (kept the current version): %v", err)
}

// checkLinuxExecutable rejects anything that isn't an ELF executable for this
// architecture, e.g. a wrong-arch or truncated binary.
func checkLinuxExecutable(path string) error {
	f, err := elf.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[runtime.GOARCH]
	if want != elf.EM_NONE && f.Machine != want {
		return fmt.Errorf("built for %v, this machine is %s", f.Machine, runtime.GOARCH)
	}
	return nil
}
