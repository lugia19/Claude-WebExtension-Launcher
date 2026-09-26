//go:build windows

package utils

import "os/exec"

// OpenInViewer opens path in its default application.
func OpenInViewer(path string) error {
	return exec.Command("explorer.exe", path).Start()
}
