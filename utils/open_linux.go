package utils

import "os/exec"

// OpenInViewer opens path in its default application.
func OpenInViewer(path string) error {
	return exec.Command("xdg-open", path).Start()
}
