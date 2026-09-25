package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

const errorSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION

// instanceRunning reports whether an instance of Claude is open. Electron's
// single-instance lock keeps "lockfile" in the instance's data folder open without
// write sharing (and deletes it on close), so opening it for writing fails with a
// sharing violation exactly while the instance runs.
func instanceRunning(instance string) bool {
	f, err := os.OpenFile(filepath.Join(claudeUserDataDir(instance), "lockfile"), os.O_WRONLY, 0)
	if err == nil {
		f.Close()
		return false
	}
	return errors.Is(err, errorSharingViolation)
}
