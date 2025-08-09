package utils

import (
	"os"
	"path/filepath"
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
	return filepath.Join(execDir, relativePath)
}
