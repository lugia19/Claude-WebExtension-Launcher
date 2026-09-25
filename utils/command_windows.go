//go:build windows

package utils

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// Command is exec.Command for console tools (icacls, powershell, sc, ...). The
// launcher is a GUI-subsystem program with no console, so Windows would give every
// console child a console window of its own; this runs them without one.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}
