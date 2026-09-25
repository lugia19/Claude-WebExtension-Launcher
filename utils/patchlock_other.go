//go:build !windows

package utils

import (
	"os"
	"syscall"
	"time"
)

// SettingsLockName guards settings.json's read-modify-write across launchers.
const SettingsLockName = "settings"

// PatchLock is an acquired cross-process lock: an flock on a file in the launcher's
// data directory. The kernel drops it when the holder exits, so a launcher that dies
// mid-patch never deadlocks the ones waiting behind it.
type PatchLock struct {
	file *os.File
}

// AcquirePatchLock blocks until the named lock is acquired or the timeout elapses.
// Returns (lock, true) on success, or (nil, false) on timeout/failure.
func AcquirePatchLock(name string, timeout time.Duration) (*PatchLock, bool) {
	f, err := os.OpenFile(ResolvePath(name+".lock"), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, false
	}

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &PatchLock{file: f}, true
		}
		if err != syscall.EWOULDBLOCK || time.Now().After(deadline) {
			f.Close()
			return nil, false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Release unlocks and closes the lock file.
func (l *PatchLock) Release() {
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
}
