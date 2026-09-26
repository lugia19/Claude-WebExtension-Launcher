package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"claude-webext-patcher/utils"
)

// On macOS (see shortcuts.go):
//   - An instance's menu entry is a small app next to the installed launcher,
//     "Claude (<name>).app", whose only content is a script that opens the launcher for
//     that instance. The launcher makes it on this Mac, so it's never quarantined.
//   - A login entry is a LaunchAgent in ~/Library/LaunchAgents that opens the launcher
//     (for the instance) at login.

// launcherHasMenuEntry: the launcher's installed app already is its menu entry.
const launcherHasMenuEntry = false

const (
	bundleIDPrefix   = "com.lugia19.claudewebextlauncher" // PACKAGE_NAME in build-all.sh
	instanceBundleID = bundleIDPrefix + ".instance."      // + bundleIDPart(name)
	loginLabel       = bundleIDPrefix + ".login"          // + "." + escapeEntry(name) for an instance
	minMacOS         = "12.0"                             // MIN_MACOS in build-all.sh
	shimExecutable   = "launch"
	lsregister       = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
)

func userDir(parts ...string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(append([]string{home}, parts...)...)
}

// menuApp is an instance's app, in the installed launcher's Applications folder.
func menuApp(entry string) string {
	return filepath.Join(filepath.Dir(installedLauncher()), entryName(entry)+".app")
}

// menuApps lists the instances' apps the launcher made.
func menuApps() []string {
	apps, _ := filepath.Glob(menuApp("*"))
	var ours []string
	for _, app := range apps {
		if isOurApp(app) {
			ours = append(ours, app)
		}
	}
	return ours
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

func hasMenuEntry(entry string) bool { return isOurApp(menuApp(entry)) }

func addMenuEntry(entry string) error {
	app := menuApp(entry)
	if fileExists(app) && !isOurApp(app) {
		return fmt.Errorf("%s already exists and isn't the launcher's", app)
	}
	icon, err := EmbeddedFS.ReadFile("resources/icons/app.icns")
	if err != nil {
		return err
	}
	files := map[string][]byte{
		"Contents/Info.plist":              []byte(shimInfoPlist(entry)),
		"Contents/MacOS/" + shimExecutable: []byte(shimScript(entry)),
		"Contents/Resources/app.icns":      icon,
	}
	if bundleMatches(app, files) {
		return nil // already up to date: skip rebuilding and re-signing it
	}

	tmp, err := os.MkdirTemp("", "claude-webext-shim")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	built := filepath.Join(tmp, filepath.Base(app))
	for rel, data := range files {
		path := filepath.Join(built, filepath.FromSlash(rel))
		mode := os.FileMode(0644)
		if rel == "Contents/MacOS/"+shimExecutable {
			mode = 0755
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, mode); err != nil {
			return err
		}
	}
	if err := utils.InstallAppBundle(built, app); err != nil {
		return err
	}
	// Best effort, after the swap (which drops extended attributes, where a script's
	// signature lives): an ad-hoc signature, and Launchpad/Spotlight seeing it now.
	exec.Command("codesign", "--force", "--sign", "-", app).Run()
	exec.Command(lsregister, "-f", app).Run()
	return nil
}

// bundleMatches reports whether the bundle at app already holds exactly these files.
func bundleMatches(app string, files map[string][]byte) bool {
	for rel, data := range files {
		existing, err := os.ReadFile(filepath.Join(app, filepath.FromSlash(rel)))
		if err != nil || !bytes.Equal(existing, data) {
			return false
		}
	}
	return true
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

// agentLabels lists the labels of the login entries the launcher made.
func agentLabels() []string {
	paths, _ := filepath.Glob(agentPath(loginLabel + "*"))
	labels := make([]string, len(paths))
	for i, p := range paths {
		labels[i] = strings.TrimSuffix(filepath.Base(p), ".plist")
	}
	return labels
}

func hasStartup(entry string) bool { return fileExists(agentPath(agentLabel(entry))) }

func setStartup(entry string, on bool) error {
	label := agentLabel(entry)
	if !on {
		return removeAgent(label)
	}
	// Not loaded now: it runs at the next login.
	return writeIfChanged(agentPath(label), agentPlist(label, entry))
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
	var quoted []string
	for _, a := range openCommand(entry) {
		quoted = append(quoted, shellQuote(a))
	}
	return "#!/bin/sh\nexec " + strings.Join(quoted, " ") + "\n"
}

// bundleIDPart makes an instance name fit a bundle identifier, which allows only
// letters, digits, - and .: anything else, and - itself, becomes -xx, so distinct
// names never share an identifier.
func bundleIDPart(entry string) string {
	var b strings.Builder
	for i := 0; i < len(entry); i++ {
		c := entry[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "-%02x", c)
		}
	}
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shimInfoPlist is the Info.plist of an instance's app. LSUIElement keeps it out of
// the Dock: it exits as soon as it has opened the launcher.
func shimInfoPlist(entry string) string {
	return plistDoc(
		plistKey("CFBundleExecutable", shimExecutable),
		plistKey("CFBundleIdentifier", instanceBundleID+bundleIDPart(entry)),
		plistKey("CFBundleName", entryName(entry)),
		plistKey("CFBundleDisplayName", entryName(entry)),
		plistKey("CFBundleIconFile", "app.icns"),
		plistKey("CFBundlePackageType", "APPL"),
		plistKey("LSMinimumSystemVersion", minMacOS),
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
