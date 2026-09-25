//go:build windows

package utils

import (
	"os/exec"
	"syscall"
	"unsafe"
)

// OpenInViewer opens path in its default application.
func OpenInViewer(path string) error {
	return exec.Command("explorer.exe", path).Start()
}

var procMessageBoxW = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

// ShowErrorDialog shows a native error message box. Used when the status window
// couldn't open.
func ShowErrorDialog(title, message string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(message)
	const mbIconError = 0x10
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError)
}
