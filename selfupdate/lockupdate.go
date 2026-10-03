//go:build windows || linux

package selfupdate

import (
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"time"
)

// lockUpdate takes a per-user cross-process lock for the update, so launchers started
// together don't replace the executable at the same time. installUpdate keeps it
// until it restarts (exiting frees it). If a launcher that held the lock before us
// already replaced the executable, restart into the new version instead of
// downloading the same update again.
func lockUpdate() (func(), bool) {
	lock, ok := utils.AcquirePatchLock(updateLockName, 5*time.Minute)
	if !ok {
		return nil, false
	}

	exe, err := os.Executable()
	if err == nil && startedFrom != nil {
		if current, err := os.Stat(exe); err == nil && !os.SameFile(startedFrom, current) {
			fmt.Println("Another launcher already installed the update, restarting...")
			lock.Release()
			err := restart(exe)
			fmt.Printf("Warning: could not restart into the updated launcher: %v\n", err)
			return nil, false
		}
	}
	return lock.Release, true
}
