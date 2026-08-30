//go:build linux

package patcher

import "runtime"

var cachedHostArch string

// HostArch returns the Linux CPU architecture as the name Claude's .deb pool
// uses for it: "amd64" (x86_64) or "arm64" (AArch64). The Packages index and
// the pool filename go by these exact names (binary-amd64/binary-arm64,
// claude-desktop_<ver>_<arch>.deb), so this must stay aligned with them. On an
// arm64 host the launcher installs native arm64 Claude rather than an amd64
// build.
func HostArch() string {
	if cachedHostArch != "" {
		return cachedHostArch
	}
	switch runtime.GOARCH {
	case "arm64":
		cachedHostArch = "arm64"
	default:
		cachedHostArch = "amd64"
	}
	return cachedHostArch
}
