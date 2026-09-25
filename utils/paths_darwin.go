package utils

import (
	"os"
	"path/filepath"
)

// ResolvePath resolves a path inside the launcher's data directory. On macOS this is
// in Application Support rather than inside the (read-only, signed) launcher bundle.
func ResolvePath(relativePath string) string {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, "Library", "Application Support", "Claude WebExtension Launcher")
	os.MkdirAll(dataDir, 0755)
	return filepath.Join(dataDir, relativePath)
}
