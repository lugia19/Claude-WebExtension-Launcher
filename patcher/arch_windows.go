//go:build windows

package patcher

import (
	"syscall"
	"unsafe"
)

// IMAGE_FILE_MACHINE_ARM64; returned by IsWow64Process2 as the native machine on ARM64 hosts.
const imageFileMachineARM64 = 0xAA64

var cachedHostArch string

// HostArch returns the native machine architecture of the host: "arm64" on a native ARM64
// machine, otherwise "x64". It uses IsWow64Process2 so the answer is correct even when this
// (amd64) process is itself running under x64 emulation on ARM64 Windows — which is exactly
// the case we care about, since the launcher ships only as amd64.
func HostArch() string {
	if cachedHostArch != "" {
		return cachedHostArch
	}
	cachedHostArch = detectHostArch()
	return cachedHostArch
}

func detectHostArch() string {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("IsWow64Process2")
	if err := proc.Find(); err != nil {
		// IsWow64Process2 is available on Windows 10 1709+. If it's somehow missing,
		// fall back to x64 (the only arch the launcher binary is built for).
		return "x64"
	}

	curProc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentProcess")
	handle, _, _ := curProc.Call()

	var processMachine, nativeMachine uint16
	ret, _, _ := proc.Call(
		handle,
		uintptr(unsafe.Pointer(&processMachine)),
		uintptr(unsafe.Pointer(&nativeMachine)),
	)
	if ret == 0 {
		return "x64"
	}
	if nativeMachine == imageFileMachineARM64 {
		return "arm64"
	}
	return "x64"
}
