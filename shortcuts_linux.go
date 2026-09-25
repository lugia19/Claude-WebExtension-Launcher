package main

import (
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func menuEntrySupported() bool { return true }

func xdgDir(env string, fallback ...string) string {
	if dir := os.Getenv(env); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(append([]string{home}, fallback...)...)
}

func applicationsDir() string {
	return filepath.Join(xdgDir("XDG_DATA_HOME", ".local", "share"), "applications")
}
func autostartDir() string { return filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "autostart") }

// escapedFileChars are escaped in entry file names: anything outside [A-Za-z0-9.-],
// including the escape character _ itself, so distinct instances never share a file.
var escapedFileChars = regexp.MustCompile(`[^A-Za-z0-9.-]`)

// entryFile is the .desktop file name for an entry (see shortcuts.go).
func entryFile(entry string) string {
	if entry == launcherEntry {
		return "claude-webext-launcher.desktop"
	}
	escaped := escapedFileChars.ReplaceAllStringFunc(entry, func(c string) string {
		var b strings.Builder
		for i := 0; i < len(c); i++ {
			fmt.Fprintf(&b, "_%02x", c[i]) // every byte as _xx, so decoding is unambiguous
		}
		return b.String()
	})
	return "claude-webext-launcher-" + escaped + ".desktop"
}

func hasMenuEntry(instance string) bool {
	return fileExists(filepath.Join(applicationsDir(), entryFile(instance)))
}
func hasStartup(instance string) bool {
	return fileExists(filepath.Join(autostartDir(), entryFile(instance)))
}

func addMenuEntry(instance string) error {
	return writeLauncherEntry(filepath.Join(applicationsDir(), entryFile(instance)), instance)
}

func removeMenuEntry(instance string) error {
	err := removeIfExists(filepath.Join(applicationsDir(), entryFile(instance)))
	refreshDesktopDatabase()
	return err
}

func setStartup(instance string, on bool) error {
	path := filepath.Join(autostartDir(), entryFile(instance))
	if on {
		return writeLauncherEntry(path, instance)
	}
	return removeIfExists(path)
}

// writeLauncherEntry writes a .desktop entry that runs the launcher (so updates
// happen) for instance.
func writeLauncherEntry(path, instance string) error {
	exe, err := launcherPath()
	if err != nil {
		return err
	}
	icon, err := iconPath()
	if err != nil {
		return err
	}
	content := desktopEntry(
		desktopField{"Type", "Application"},
		desktopField{"Name", entryName(instance)},
		desktopField{"Comment", "Claude Desktop with web extensions"},
		desktopField{"Exec", desktopExec("", append([]string{exe}, entryArgs(instance)...)...)},
		desktopField{"Icon", icon},
		desktopField{"Terminal", "false"},
		desktopField{"Categories", "Network;"},
	)
	if err := writeIfChanged(path, content); err != nil {
		return err
	}
	refreshDesktopDatabase()
	return nil
}

// writeLinkHandler writes the hidden claude:// handler entry, pointing at the patched
// Claude with the main instance: a link opened while Claude runs is handed to the
// running instance by Claude's single-instance lock. Called on every Linux launch
// (after migrateMainInstance, so it follows the main instance's current name); it
// only writes when something changed.
func writeLinkHandler() {
	icon, err := iconPath()
	if err != nil {
		fmt.Printf("Warning: could not write the app icon: %v\n", err)
	}
	content := desktopEntry(
		desktopField{"Type", "Application"},
		desktopField{"Name", shortcutName},
		desktopField{"NoDisplay", "true"},
		desktopField{"Exec", desktopExec("%U", claudeExecutablePath(), "--instance="+mainInstance)},
		desktopField{"Icon", icon},
		desktopField{"Terminal", "false"},
		desktopField{"MimeType", "x-scheme-handler/claude;"},
	)
	path := filepath.Join(applicationsDir(), patcher.LinkHandlerDesktop)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return
	}
	if err := writeIfChanged(path, content); err != nil {
		fmt.Printf("Warning: could not write the claude:// link handler: %v\n", err)
		return
	}
	refreshDesktopDatabase()
	fmt.Println("Wrote the claude:// link handler entry")
}

// launcherPath is the launcher executable, with symlinks resolved so the entry keeps
// working however it was started.
func launcherPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// iconPath writes the launcher icon into the data folder (entries need a file path)
// and returns it.
func iconPath() (string, error) {
	data, err := EmbeddedFS.ReadFile("resources/icons/app.png")
	if err != nil {
		return "", err
	}
	path := utils.ResolvePath("app.png")
	return path, writeIfChanged(path, string(data))
}

func writeIfChanged(path, content string) error {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// refreshDesktopDatabase updates the MIME cache for the applications folder, if the
// tool is there; desktops pick up new entries without it too, just less promptly.
func refreshDesktopDatabase() {
	if _, err := exec.LookPath("update-desktop-database"); err == nil {
		exec.Command("update-desktop-database", applicationsDir()).Run()
	}
}
