package utils

import (
	"os"
	"path/filepath"
	"runtime"
)

var execDir string

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
