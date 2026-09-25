//go:build windows

package utils

import (
	"os"
	"syscall"
)

var (
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procAttachConsole    = kernel32.NewProc("AttachConsole")
	procAllocConsole     = kernel32.NewProc("AllocConsole")
)

const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS ((DWORD)-1)

// EnsureConsole gives a GUI-subsystem build a console to print to: the one it was
// started from (e.g. cmd.exe) if any, otherwise a new window. No-op if the process
// already has a console (console-subsystem builds, e.g. plain `go build`).
func EnsureConsole() {
	if hwnd, _, _ := procGetConsoleWindow.Call(); hwnd != 0 {
		return
	}
	if ok, _, _ := procAttachConsole.Call(attachParentProcess); ok == 0 {
		if ok, _, _ := procAllocConsole.Call(); ok == 0 {
			return
		}
	}

	// The standard handles were set up before the console existed; point them at it.
	if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout = out
		os.Stderr = out
		syscall.Stdout = syscall.Handle(out.Fd())
		syscall.Stderr = syscall.Handle(out.Fd())
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in
		syscall.Stdin = syscall.Handle(in.Fd())
	}
}
