package utils

import (
	"os"
	"path/filepath"
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

// DataDir is the launcher's per-user folder: its logs, settings.json, and (on
// Windows and Linux) the installed launcher itself.
func DataDir() string {
	return logDir()
}
