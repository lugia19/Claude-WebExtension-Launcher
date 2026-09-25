package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"claude-webext-patcher/utils"
)

const appBundleName = "Claude_WebExtension_Launcher.app"

// installedLauncher is where the launcher installs itself: the user's own Applications
// folder, which needs no admin rights.
func installedLauncher() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Applications", appBundleName)
}

// runningLauncher is the app bundle this process runs from. A bare binary (not inside
// an .app, e.g. a development build) isn't installed.
func runningLauncher() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	app := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // <app>/Contents/MacOS/<exe>
	if !strings.HasSuffix(app, ".app") {
		return "", fmt.Errorf("%s isn't inside an app bundle", exe)
	}
	return app, nil
}

// launcherBinary is the file holding the launcher's code, inside the bundle.
func launcherBinary(app string) string {
	return filepath.Join(app, "Contents", "MacOS", "Claude_WebExtension_Launcher")
}

func samePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

func installCopy(running, installed string) error {
	return utils.InstallAppBundle(running, installed)
}

// handOff replaces this process with the installed launcher. Returns only if that
// failed.
func handOff(target string, args []string, debug bool) error {
	bin := launcherBinary(target)
	return syscall.Exec(bin, append([]string{bin}, args...), os.Environ())
}
