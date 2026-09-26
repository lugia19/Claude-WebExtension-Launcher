package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// On macOS (see shortcuts.go):
//   - An instance's menu entry is a small app in ~/Applications, "Claude (<name>).app",
//     whose only content is a script that opens the launcher for that instance. The
//     launcher makes it on this Mac, so it's never quarantined. The launcher's own
//     menu entry is its installed app, so there's none to manage for it.
//   - A login entry is a LaunchAgent in ~/Library/LaunchAgents that opens the launcher
//     (for the instance) at login.

const (
	bundleIDPrefix   = "com.lugia19.claudewebextlauncher"
	instanceBundleID = bundleIDPrefix + ".instance." // + escapeEntry(name)
	loginLabel       = bundleIDPrefix + ".login"     // + "." + escapeEntry(name) for an instance
	shimExecutable   = "launch"
	lsregister       = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
)

func menuEntrySupported(entry string) bool { return entry != launcherEntry }

func userDir(parts ...string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(append([]string{home}, parts...)...)
}

// menuApp is an instance's app in ~/Applications.
func menuApp(entry string) string {
	return userDir("Applications", entryName(entry)+".app")
}

// isOurApp reports whether app is one the launcher made, so an unrelated app that
// happens to have the same name is never replaced or removed.
func isOurApp(app string) bool {
	if !fileExists(filepath.Join(app, "Contents", "MacOS", shimExecutable)) {
		return false
	}
	plist, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	return err == nil && bytes.Contains(plist, []byte("<string>"+instanceBundleID))
}

func hasMenuEntry(entry string) bool {
	return menuEntrySupported(entry) && isOurApp(menuApp(entry))
}

func addMenuEntry(entry string) error {
	if !menuEntrySupported(entry) {
		return nil
	}
	app := menuApp(entry)
	if fileExists(app) && !isOurApp(app) {
		return fmt.Errorf("%s already exists and isn't the launcher's", app)
	}
	icon, err := EmbeddedFS.ReadFile("resources/icons/app.icns")
	if err != nil {
		return err
	}

	// Built next to it, then swapped in, so a failure leaves the old one working.
	tmp := app + ".new"
	os.RemoveAll(tmp)
	files := []struct {
		path string
		data []byte
		mode os.FileMode
	}{
		{"Contents/Info.plist", []byte(shimInfoPlist(entry)), 0644},
		{"Contents/MacOS/" + shimExecutable, []byte(shimScript(entry)), 0755},
		{"Contents/Resources/app.icns", icon, 0644},
	}
	for _, f := range files {
		path := filepath.Join(tmp, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.data, f.mode); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.RemoveAll(app); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, app); err != nil {
		os.RemoveAll(tmp)
		return err
	}

	// Best effort: an ad-hoc signature, and telling Launchpad/Spotlight about it now
	// rather than whenever they next look.
	exec.Command("codesign", "--force", "--sign", "-", app).Run()
	exec.Command(lsregister, "-f", app).Run()
	return nil
}

func removeMenuEntry(entry string) error {
	if !hasMenuEntry(entry) {
		return nil
	}
	return os.RemoveAll(menuApp(entry))
}

// agentLabel is an entry's LaunchAgent label, which is also its file name.
func agentLabel(entry string) string {
	if entry == launcherEntry {
		return loginLabel
	}
	return loginLabel + "." + escapeEntry(entry)
}

func agentPath(label string) string {
	return userDir("Library", "LaunchAgents", label+".plist")
}

func hasStartup(entry string) bool { return fileExists(agentPath(agentLabel(entry))) }

func setStartup(entry string, on bool) error {
	label := agentLabel(entry)
	if !on {
		return removeAgent(label)
	}
	path := agentPath(label)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// Not loaded now: it runs at the next login.
	return os.WriteFile(path, []byte(agentPlist(label, entry)), 0644)
}

// removeAgent deletes a LaunchAgent, and unloads it if this login loaded it.
func removeAgent(label string) error {
	err := removeIfExists(agentPath(label))
	exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Run()
	return err
}

// openCommand opens the installed launcher for an entry. open -n starts it even when
// a launcher window is already open, and as the launcher's own app (its name and icon).
func openCommand(entry string) []string {
	cmd := []string{"/usr/bin/open", "-n", "-a", installedLauncher()}
	if args := entryArgs(entry); len(args) > 0 {
		cmd = append(append(cmd, "--args"), args...)
	}
	return cmd
}

func shimScript(entry string) string {
	quoted := make([]string, 0, len(openCommand(entry)))
	for _, a := range openCommand(entry) {
		quoted = append(quoted, shellQuote(a))
	}
	return "#!/bin/sh\nexec " + strings.Join(quoted, " ") + "\n"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shimInfoPlist is the Info.plist of an instance's app. LSUIElement keeps it out of
// the Dock: it exits as soon as it has opened the launcher.
func shimInfoPlist(entry string) string {
	return plistDoc(
		plistKey("CFBundleExecutable", shimExecutable),
		plistKey("CFBundleIdentifier", instanceBundleID+escapeEntry(entry)),
		plistKey("CFBundleName", entryName(entry)),
		plistKey("CFBundleDisplayName", entryName(entry)),
		plistKey("CFBundleIconFile", "app.icns"),
		plistKey("CFBundlePackageType", "APPL"),
		plistKey("LSMinimumSystemVersion", "12.0"),
		"\t<key>LSUIElement</key>\n\t<true/>\n",
	)
}

func agentPlist(label, entry string) string {
	var args strings.Builder
	for _, a := range openCommand(entry) {
		args.WriteString("\t\t<string>" + xmlEscape(a) + "</string>\n")
	}
	return plistDoc(
		plistKey("Label", label),
		"\t<key>ProgramArguments</key>\n\t<array>\n"+args.String()+"\t</array>\n",
		"\t<key>RunAtLoad</key>\n\t<true/>\n",
		plistKey("LimitLoadToSessionType", "Aqua"),
	)
}

func plistKey(key, value string) string {
	return "\t<key>" + key + "</key>\n\t<string>" + xmlEscape(value) + "</string>\n"
}

func plistDoc(entries ...string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
` + strings.Join(entries, "") + "</dict>\n</plist>\n"
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
