//go:build !windows

package main

import (
	"os"
	"os/exec"
	"time"
)

const (
	// patchLockName serializes the worker across launchers started together (e.g.
	// several named instances), which share the staging, asar-temp and
	// web-extensions paths.
	patchLockName = "patch"
	// patchLockTimeout is generous: a real update downloads Claude (~175-250 MB).
	patchLockTimeout = 10 * time.Minute
)

// ensureConsole is a no-op outside Windows, which has no GUI/console subsystem split.
func ensureConsole() {}

// startWorker runs the worker as a child process (same user, no elevation) and
// returns its exit code. Its output joins the launcher's (the log file).
func startWorker(args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return -1, err
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// Worker hooks: nothing to set up or tear down outside Windows.
func workerBefore() error   { return nil }
func workerAfter()          {}
func registerCowork() error { return nil }
