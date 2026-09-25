//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// instanceRunning reports whether an instance of Claude is open. Electron's
// single-instance lock is a "SingletonLock" symlink in the instance's data folder,
// pointing at "<hostname>-<pid>" of the process holding it. It can be left behind by
// a crash, so the process is checked too.
func instanceRunning(instance string) bool {
	target, err := os.Readlink(filepath.Join(claudeUserDataDir(instance), "SingletonLock"))
	if err != nil {
		return false
	}
	i := strings.LastIndex(target, "-")
	if i < 0 {
		return true // unknown format: err on the side of "open"
	}
	if host, _ := os.Hostname(); host != "" && target[:i] != host {
		return true // held from another machine (shared home folder)
	}
	pid, err := strconv.Atoi(target[i+1:])
	if err != nil {
		return true
	}
	// Signal 0 checks the process exists; EPERM means it does, but isn't ours.
	err = syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
