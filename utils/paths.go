package utils

import (
	"os"
	"path/filepath"
	"runtime"
)

var execDir string

const WindowsInstallDir = `C:\Program Files\WindowsApps\ClaudeWebExtLauncher`

func init() {
	execPath, err := os.Executable()
	if err != nil {
		panic("Failed to get executable path: " + err.Error())
	}
	execDir = filepath.Dir(execPath)
}

func GetExecutableDir() string {
	return execDir
}

// ResolvePath resolves a path relative to the launcher's directory.
// Used for launcher-local files: node_modules, temp zips, asar-temp, etc.
func ResolvePath(relativePath string) string {
	if runtime.GOOS == "darwin" {
		// On macOS, use Application Support directory instead of bundle
		home, _ := os.UserHomeDir()
		dataDir := filepath.Join(home, "Library", "Application Support", "Claude WebExtension Launcher")
		os.MkdirAll(dataDir, 0755)
		return filepath.Join(dataDir, relativePath)
	}
	// Windows and other platforms: use executable directory
	return filepath.Join(execDir, relativePath)
}

// ResolveInstallPath resolves a path relative to the app install directory.
// On Windows, this is in WindowsApps. On macOS, same as ResolvePath.
func ResolveInstallPath(relativePath string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(WindowsInstallDir, relativePath)
	}
	return ResolvePath(relativePath)
}
