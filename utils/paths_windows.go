//go:build windows

package utils

import (
	"os"
	"path/filepath"
)

// ResolvePath resolves a path relative to the launcher's directory.
// Used for launcher-local files: node_modules, temp zips, asar-temp, etc.
func ResolvePath(relativePath string) string {
	return filepath.Join(execDir, relativePath)
}

// ResolveInstallPath resolves a path relative to the app install directory.
// On Windows, this is in WindowsApps.
func ResolveInstallPath(relativePath string) string {
	return filepath.Join(WindowsInstallDir, relativePath)
}

// logDir is per-user rather than next to the launcher, so the elevated worker and the
// unelevated launcher write to the same, user-readable place.
func logDir() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "ClaudeWebExtLauncher")
}
