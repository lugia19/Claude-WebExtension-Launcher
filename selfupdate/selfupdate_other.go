//go:build !windows

package selfupdate

import "os"

// finishUpdateIfNeeded removes the previous launcher that Linux's installUpdate keeps
// as a rollback until the new binary has started. macOS hands the new bundle to the
// user instead, so there is nothing to clean up there.
func finishUpdateIfNeeded(exePath string) {
	os.Remove(exePath + ".old")
}
