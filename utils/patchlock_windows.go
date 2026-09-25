//go:build windows

package utils

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// SettingsLockName guards settings.json's read-modify-write across launchers.
const SettingsLockName = `Local\ClaudeWebExtLauncher-Settings`

// Reuse the kernel32 handle and WaitForSingleObject/CloseHandle procs declared in
// admin_windows.go (same package); add the mutex-specific entry points here.
var (
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	procReleaseMutex = kernel32.NewProc("ReleaseMutex")
)

const (
	waitObject0    = 0x00000000
	waitAbandoned  = 0x00000080
	infiniteMillis = 0xFFFFFFFF
)

// PatchLock is an acquired cross-process named mutex.
type PatchLock struct {
	handle uintptr
}

// AcquirePatchLock creates or opens a named mutex and blocks until it is acquired
// or the timeout elapses. The name should include a namespace prefix (e.g.
// `Local\...`). Returns (lock, true) on success, or (nil, false) on timeout/failure.
//
// WAIT_ABANDONED — a previous owner crashed without releasing — counts as acquired,
// so a patcher that dies mid-run never deadlocks the instances waiting behind it.
func AcquirePatchLock(name string, timeout time.Duration) (*PatchLock, bool) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false
	}

	handle, _, _ := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if handle == 0 {
		return nil, false
	}

	ms := uintptr(infiniteMillis)
	if timeout >= 0 {
		ms = uintptr(timeout.Milliseconds())
	}

	// A mutex is owned by the OS thread that acquired it, and only that thread can
	// release it; pin this goroutine to its thread until Release (which must be called
	// from the same goroutine), or Go could move it and ReleaseMutex would fail,
	// leaving the mutex held.
	runtime.LockOSThread()
	ret, _, _ := procWaitForSingleObject.Call(handle, ms)
	switch uint32(ret) {
	case waitObject0, waitAbandoned:
		return &PatchLock{handle: handle}, true
	default:
		// Timed out or failed: we own the handle but not the lock, so close it.
		runtime.UnlockOSThread()
		procCloseHandle.Call(handle)
		return nil, false
	}
}

// Release releases the mutex and closes its handle. Call it from the goroutine that
// acquired the lock (see AcquirePatchLock).
func (l *PatchLock) Release() {
	if l == nil || l.handle == 0 {
		return
	}
	procReleaseMutex.Call(l.handle)
	procCloseHandle.Call(l.handle)
	l.handle = 0
	runtime.UnlockOSThread()
}
