//go:build !windows

package selfupdate

// finishUpdateIfNeeded is a no-op on non-Windows platforms.
// Windows stages a .new.exe swap across process restarts; macOS uses a shell
// workflow and Linux replaces the binary in place (see installUpdate in
// selfupdate_linux.go / selfupdate_darwin.go).
func finishUpdateIfNeeded(exePath string) {}
