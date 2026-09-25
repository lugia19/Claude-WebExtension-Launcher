//go:build !windows

package utils

// ResolveInstallPath resolves a path relative to the app install directory.
// On non-Windows, this is the same as ResolvePath.
func ResolveInstallPath(relativePath string) string {
	return ResolvePath(relativePath)
}
