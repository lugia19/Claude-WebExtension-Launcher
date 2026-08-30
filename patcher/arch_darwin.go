//go:build darwin

package patcher

import "runtime"

var cachedHostArch string

// HostArch returns the native macOS architecture as Claude's build uses it.
// macOS resolution does not vary by arch (the manifest is arch-agnostic), but
// the symbol must exist so the shared patcher compiles on all platforms.
func HostArch() string {
	if cachedHostArch != "" {
		return cachedHostArch
	}
	switch runtime.GOARCH {
	case "arm64":
		cachedHostArch = "arm64"
	default:
		cachedHostArch = "x64"
	}
	return cachedHostArch
}
