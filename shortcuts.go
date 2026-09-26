package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// App-menu and startup entries: the Start Menu / Startup folder shortcuts on Windows,
// .desktop files on Linux, and on macOS a small app per instance in ~/Applications
// plus LaunchAgents for login (the launcher's own app is its menu entry there, see
// launcherHasMenuEntry). The platform parts are in shortcuts_<os>.go; they take an entry
// key: launcherEntry, or an instance name.
//
// There are two kinds of entries. The launcher's runs it with no arguments: it
// launches the main instance, or shows the instance list when that's on. An
// instance's (the main one's too) launches that instance directly.

// shortcutName is what the entries are called. It's also the name the old
// Toggle-*.bat scripts used, so shortcuts they created are recognized (as the
// launcher's entry).
const shortcutName = "Claude Desktop (Extended)"

// launcherEntry is the entry key of the launcher's own entry.
const launcherEntry = ""

// entryName is an entry's display name: shortcutName for the launcher, "Claude
// (<instance>)" for an instance.
func entryName(entry string) string {
	if entry == launcherEntry {
		return shortcutName
	}
	return "Claude (" + entry + ")"
}

// entryArgs are the launcher arguments an entry passes.
func entryArgs(entry string) []string {
	if entry == launcherEntry {
		return nil
	}
	return []string{"--instance=" + entry}
}

// escapedChars are escaped by escapeEntry: anything outside [A-Za-z0-9.-], including
// the escape character _ itself, so distinct instances never share a name.
var escapedChars = regexp.MustCompile(`[^A-Za-z0-9.-]`)

// escapeEntry makes an instance name safe for file names and identifiers, every
// escaped byte as _xx so decoding is unambiguous.
func escapeEntry(entry string) string {
	return escapedChars.ReplaceAllStringFunc(entry, func(c string) string {
		var b strings.Builder
		for i := 0; i < len(c); i++ {
			fmt.Fprintf(&b, "_%02x", c[i])
		}
		return b.String()
	})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeIfChanged writes content to path (making its folder), unless it's already there.
func writeIfChanged(path, content string) error {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == content {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
