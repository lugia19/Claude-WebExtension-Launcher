//go:build !windows

package selfupdate

// finishUpdateIfNeeded is a no-op on non-Windows platforms: macOS hands the new bundle
// to the user, and Linux replaces the binary in place and re-execs.
func finishUpdateIfNeeded(exePath string) {}
