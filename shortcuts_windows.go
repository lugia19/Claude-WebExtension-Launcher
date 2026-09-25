//go:build windows

package main

import (
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func menuEntrySupported() bool { return true }

func startMenuDir() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs")
}

func menuShortcut(instance string) string {
	return filepath.Join(startMenuDir(), entryName(instance)+".lnk")
}

func startupShortcut(instance string) string {
	return filepath.Join(startMenuDir(), "Startup", entryName(instance)+".lnk")
}

func hasMenuEntry(instance string) bool { return fileExists(menuShortcut(instance)) }
func hasStartup(instance string) bool   { return fileExists(startupShortcut(instance)) }

func addMenuEntry(instance string) error { return createShortcut(menuShortcut(instance), instance) }

func removeMenuEntry(instance string) error { return removeIfExists(menuShortcut(instance)) }

func setStartup(instance string, on bool) error {
	if on {
		return createShortcut(startupShortcut(instance), instance)
	}
	return removeIfExists(startupShortcut(instance))
}

// createShortcut writes a .lnk to the launcher through WScript.Shell, as the old
// Toggle-*.bat scripts did. Values go in through environment variables so no path
// needs quoting inside the PowerShell command.
func createShortcut(lnk, instance string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lnk), 0755); err != nil {
		return err
	}
	args := entryArgs(instance)
	for i, a := range args {
		args[i] = syscall.EscapeArg(a)
	}

	cmd := utils.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`$s = (New-Object -ComObject WScript.Shell).CreateShortcut($env:CWL_LNK); `+
			`$s.TargetPath = $env:CWL_TARGET; $s.Arguments = $env:CWL_ARGS; `+
			`$s.WorkingDirectory = $env:CWL_DIR; $s.Description = $env:CWL_NAME; $s.Save()`)
	cmd.Env = append(os.Environ(),
		"CWL_LNK="+lnk,
		"CWL_TARGET="+exe,
		"CWL_ARGS="+strings.Join(args, " "),
		"CWL_DIR="+filepath.Dir(exe),
		"CWL_NAME="+entryName(instance),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating %s: %v\n%s", lnk, err, out)
	}
	if !fileExists(lnk) {
		return fmt.Errorf("creating %s: no shortcut was written", lnk)
	}
	return nil
}
