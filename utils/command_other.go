//go:build !windows

package utils

import "os/exec"

// Command is exec.Command; the console-window suppression it adds is Windows-only.
func Command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
