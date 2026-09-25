package main

// The launcher installs itself to a fixed per-user place (installedLauncher, per OS)
// and runs from there: the copy the user downloaded only hands over to it, installing
// or upgrading it first when needed. That way the shortcuts, the elevated worker and
// self-updates always use the same path, however many times and wherever the launcher
// is downloaded.

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"claude-webext-patcher/selfupdate"
	"claude-webext-patcher/utils"
)

// installedFromFlag is added to the arguments when handing over to the installed copy;
// its value is the copy that handed over.
const installedFromFlag = "installed-from"

// installedVersionFile records the installed launcher's version, so a downloaded copy
// can tell whether it's newer without running it. The installed copy rewrites it on
// every start, so self-updates keep it right.
func installedVersionFile() string {
	return filepath.Join(utils.DataDir(), "launcher-version.txt")
}

// installSelf makes sure the installed copy is current. It returns where to hand over
// to and this copy's path, or "" if this process should just carry on: it is the
// installed copy, it isn't running as a proper release build (e.g. a bare binary
// on macOS), or installing failed (then it runs from where it is, this time).
//
// handedOver says this process was itself handed over to (--installed-from). It then
// never hands over again, even if it somehow doesn't recognize itself as the installed
// copy, so a mistake here can't turn into an endless chain of launches.
func installSelf(handedOver bool) (target, from string) {
	running, err := runningLauncher()
	if err != nil {
		fmt.Printf("Not installing the launcher: %v\n", err)
		return "", ""
	}
	installed := installedLauncher()
	if !sameLocation(running, installed) && handedOver {
		fmt.Printf("Warning: handed over to %s, but running from %s; carrying on here\n", installed, running)
		return "", ""
	}
	if sameLocation(running, installed) {
		os.WriteFile(installedVersionFile(), []byte(Version), 0644)
		cleanupInstall(installed)
		writeUninstallScript()
		return "", ""
	}

	_, statErr := os.Stat(installed)
	installedVersion, _ := os.ReadFile(installedVersionFile())
	differs := func() bool { return !sameFileContent(launcherBinary(running), launcherBinary(installed)) }
	if shouldReplace(statErr == nil, Version, strings.TrimSpace(string(installedVersion)), differs) {
		fmt.Printf("Installing the launcher (%s) to %s\n", Version, installed)
		if err := os.MkdirAll(filepath.Dir(installed), 0755); err != nil {
			fmt.Printf("Warning: could not install the launcher (%v); running from %s this time\n", err, running)
			return "", ""
		}
		if err := installCopy(running, installed); err != nil {
			fmt.Printf("Warning: could not install the launcher (%v); running from %s this time\n", err, running)
			return "", ""
		}
		os.WriteFile(installedVersionFile(), []byte(Version), 0644)
	}
	fmt.Printf("Handing over to the installed launcher: %s\n", installed)
	return installed, running
}

// sameLocation reports whether two paths are the same file or folder. By identity when
// both exist: the paths can differ through symlinks (a symlinked XDG_DATA_HOME or
// ~/Applications, say). Otherwise by comparing the paths.
func sameLocation(a, b string) bool {
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(ia, ib)
	}
	return samePath(a, b)
}

// shouldReplace decides whether the running launcher should replace the installed
// one: when there's none, when it's a newer version, or when it's the same version but
// a different build (so a fresh dev build takes over). Never an older version.
func shouldReplace(installedExists bool, runningVersion, installedVersion string, differs func() bool) bool {
	if !installedExists {
		return true
	}
	if installedVersion == "" {
		return differs() // unknown version (no version file): keep it in step with us
	}
	switch c := selfupdate.CompareVersions(runningVersion, installedVersion); {
	case c > 0:
		return true
	case c < 0:
		return false
	}
	return differs()
}

// sameFileContent reports whether two files have the same contents.
func sameFileContent(a, b string) bool {
	ha, errA := fileHash(a)
	hb, errB := fileHash(b)
	return errA == nil && errB == nil && bytes.Equal(ha, hb)
}

func fileHash(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// replaceFile copies src over dst without ever leaving dst half written: the copy is
// made next to dst, the old dst is moved aside, and the copy renamed into place.
// Moving a file aside works even while it's running (on Windows too, where it can't
// be overwritten or deleted then); the old copy is removed if possible, and otherwise
// on a later start (cleanupInstall).
func replaceFile(src, dst string, mode os.FileMode) error {
	staged, old := dst+".new", dst+".old"
	if err := copyWithMode(src, staged, mode); err != nil {
		os.Remove(staged)
		return err
	}
	os.Remove(old)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			os.Remove(staged)
			return fmt.Errorf("moving the old launcher aside: %w", err)
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		os.Rename(old, dst) // put the old one back
		return fmt.Errorf("moving the new launcher in: %w", err)
	}
	os.Remove(old)
	return nil
}

func copyWithMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// cleanupInstall removes what an earlier install couldn't (an old copy that was
// still running then).
func cleanupInstall(installed string) {
	os.Remove(installed + ".old")
	os.Remove(installed + ".new")
}

// writeUninstallScript keeps this OS's uninstall script (embedded) in the launcher's
// data folder, where it stays after the download it came with is deleted. Line endings
// are normalized: cmd mishandles labels in LF-only batch files, bash chokes on CRLF.
func writeUninstallScript() {
	if uninstallScript == "" {
		return
	}
	data, err := EmbeddedFS.ReadFile("resources/" + uninstallScript)
	if err != nil {
		return
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.HasSuffix(uninstallScript, ".bat") {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	path := filepath.Join(utils.DataDir(), uninstallScript)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == text {
		return
	}
	if err := os.WriteFile(path, []byte(text), 0755); err != nil {
		fmt.Printf("Warning: could not write %s: %v\n", path, err)
	}
}

// handOffArgs are this run's arguments for the installed copy: the same ones, with
// --installed-from set to this copy.
func handOffArgs(from string) []string {
	var args []string
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(strings.TrimLeft(a, "-"), installedFromFlag+"=") {
			continue
		}
		args = append(args, a)
	}
	return append(args, "--"+installedFromFlag+"="+from)
}

// refreshShortcuts points the shortcuts that exist (the launcher's and the
// instances', menu and startup) at the installed launcher. Run after a hand-over, since
// they may point at the copy that handed over.
func refreshShortcuts() {
	entries := append([]string{launcherEntry, mainInstanceName}, utils.LoadSettings().Instances...)
	for _, e := range entries {
		if hasMenuEntry(e) {
			if err := addMenuEntry(e); err != nil {
				fmt.Printf("Warning: could not update the menu entry %q: %v\n", entryName(e), err)
			}
		}
		if hasStartup(e) {
			if err := setStartup(e, true); err != nil {
				fmt.Printf("Warning: could not update the startup entry %q: %v\n", entryName(e), err)
			}
		}
	}
}
