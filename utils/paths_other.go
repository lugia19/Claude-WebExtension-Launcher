//go:build !windows

package utils

import (
	"os"
	"path/filepath"
	"runtime"
)

// ResolvePath resolves a path relative to the launcher's directory.
// On macOS, uses the Application Support directory instead of the bundle.
// On Linux, uses the XDG data directory. Other non-Windows platforms fall
// back to the launcher's executable directory.
func ResolvePath(relativePath string) string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		dataDir := filepath.Join(home, "Library", "Application Support", "Claude WebExtension Launcher")
		os.MkdirAll(dataDir, 0755)
		return filepath.Join(dataDir, relativePath)
	}
	if runtime.GOOS == "linux" {
		dataDir := filepath.Join(home, ".local", "share", "claude-webext-launcher")
		os.MkdirAll(dataDir, 0755)
		return filepath.Join(dataDir, relativePath)
	}
	return filepath.Join(execDir, relativePath)
}

// ResolveInstallPath resolves a path relative to the app install directory.
// On non-Windows, this is the same as ResolvePath.
func ResolveInstallPath(relativePath string) string {
	return ResolvePath(relativePath)
}
