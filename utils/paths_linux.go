package utils

import (
	"os"
	"path/filepath"
)

// ResolvePath resolves a path inside the launcher's data directory:
// $XDG_DATA_HOME/claude-webext-launcher, defaulting to ~/.local/share. Keeping it out
// of the executable's directory lets the launcher binary live anywhere, including
// read-only locations.
func ResolvePath(relativePath string) string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	dataDir := filepath.Join(base, "claude-webext-launcher")
	os.MkdirAll(dataDir, 0755)
	return filepath.Join(dataDir, relativePath)
}
