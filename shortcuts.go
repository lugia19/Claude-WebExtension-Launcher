package main

// App-menu and startup entries: the Start Menu / Startup folder shortcuts on Windows,
// .desktop files on Linux. macOS has no equivalent to manage (the .app goes in
// Applications; startup is System Settings > Login Items). Each instance gets its own
// entry. The platform parts are in shortcuts_<os>.go.

// shortcutName is what the entries are called. It's also the name the old
// Toggle-*.bat scripts used, so shortcuts they created are recognized.
const shortcutName = "Claude Desktop (Extended)"

// entryName is an entry's display name: shortcutName for the default instance,
// "shortcutName - <instance>" for named ones.
func entryName(instance string) string {
	if instance == defaultInstanceName {
		return shortcutName
	}
	return shortcutName + " - " + instance
}

// entryArgs are the launcher arguments an entry passes: none for the default instance.
func entryArgs(instance string) []string {
	if instance == defaultInstanceName {
		return nil
	}
	return []string{"--instance=" + instance}
}
