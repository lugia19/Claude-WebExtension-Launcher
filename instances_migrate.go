package main

import (
	"fmt"
	"os"
)

// migrateMainInstance renames the main instance's data folder from its old name
// (Claude-modified) to the current one (Claude-Main), so it follows the same "folder
// name = instance name" pattern as the others. It's the same folder moved, so it's
// instant, and the session-sharing junctions inside keep pointing where they did.
//
// If the old instance is open (or the rename fails), this run keeps using the old
// name, so its window is the one that comes up, and the next run tries again. If both
// folders exist, nothing is merged or touched: the new one is used.
func migrateMainInstance() {
	mainInstance = migrateFolder(claudeUserDataDir(legacyMainInstanceName), claudeUserDataDir(mainInstanceName),
		func() bool { return instanceRunning(legacyMainInstanceName) })
}

// migrateFolder moves oldDir to newDir when that's safe, and returns the instance name
// to use for this run: mainInstanceName, or legacyMainInstanceName while the old
// folder is still in place.
func migrateFolder(oldDir, newDir string, running func() bool) string {
	if !dirExists(oldDir) {
		return mainInstanceName
	}
	if dirExists(newDir) {
		fmt.Printf("Warning: both %s and %s exist; using the second, leaving the first alone\n", oldDir, newDir)
		return mainInstanceName
	}
	if running() {
		fmt.Printf("The main instance is open under its old name (%s); renaming its data folder next time\n", legacyMainInstanceName)
		return legacyMainInstanceName
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		fmt.Printf("Warning: could not rename %s to %s (%v); trying again next time\n", oldDir, newDir, err)
		return legacyMainInstanceName
	}
	fmt.Printf("Renamed the main instance's data folder: %s -> %s\n", oldDir, newDir)
	// Claude keeps some config next to its data folder, in "<folder>-3p"; it moves too.
	if dirExists(oldDir+companionSuffix) && !dirExists(newDir+companionSuffix) {
		if err := os.Rename(oldDir+companionSuffix, newDir+companionSuffix); err != nil {
			fmt.Printf("Warning: could not rename %s: %v\n", oldDir+companionSuffix, err)
		}
	}
	return mainInstanceName
}

// companionSuffix names the folder Claude keeps next to an instance's data folder
// ("Claude-<name>-3p", holding claude_desktop_config.json).
const companionSuffix = "-3p"

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
