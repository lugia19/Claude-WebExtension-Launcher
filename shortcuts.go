package main

import "os"

// App-menu and startup entries: the Start Menu / Startup folder shortcuts on Windows,
// .desktop files on Linux. macOS has no equivalent to manage (the .app goes in
// Applications; startup is System Settings > Login Items). The platform parts are in
// shortcuts_<os>.go; they take an entry key: launcherEntry, or an instance name.
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
