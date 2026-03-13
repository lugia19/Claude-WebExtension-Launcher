//go:build !windows

package utils

// IsAdmin returns true on non-Windows platforms (elevation not needed).
func IsAdmin() bool {
	return true
}

// RelaunchAsAdmin is a no-op on non-Windows platforms.
func RelaunchAsAdmin() error {
	return nil
}
