//go:build windows

package main

import (
	"claude-webext-patcher/utils"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// sessionLockName serializes session-folder mutations across concurrently-launched
	// instances so the shared official install isn't repaired/junctioned by two processes at
	// once. Independent of the patch lock, which is already released by this point.
	sessionLockName    = `Local\ClaudeWebExtLauncher-Sessions`
	sessionLockTimeout = 2 * time.Minute
)

// sharedSessionFolders are the userData subfolders that hold self-contained session
// data and should be shared between the official and patched Claude installs. The
// install-specific folders (claude-code = the CLI binary, claude-code-vm = VM state)
// are deliberately NOT shared.
var sharedSessionFolders = []string{
	"local-agent-mode-sessions", // Cowork / agent-mode sessions
	"claude-code-sessions",      // Claude Code sessions
}

// SetupSessionSharing makes the official and patched Claude installs read/write a single
// physical copy of their Cowork and Code sessions, via Windows directory junctions that
// point into a neutral, launcher-owned canonical store. This avoids any two-way sync (and
// the deletion-tracking problem that comes with it): there is only ever one copy.
//
// The canonical store lives at %APPDATA%\ClaudeWebExtLauncher\shared-sessions so it
// survives uninstalling either app. On first run, existing sessions from both installs are
// merged into the store (newer-mtime wins) and each original folder is backed up before
// being replaced by a junction. Runs unelevated and is fully idempotent.
func SetupSessionSharing(instanceName string) {
	// Only the default instance shares sessions with the official install. Named instances
	// stay isolated (RepairSessionSharing keeps them that way), so the single global store is
	// only ever linked by {official, Claude-<default>}.
	if instanceName != mainInstance {
		return
	}

	appData := os.Getenv("APPDATA")
	if appData == "" {
		return
	}

	lock, locked := utils.AcquirePatchLock(sessionLockName, sessionLockTimeout)
	if !locked {
		fmt.Println("Warning: timed out waiting to set up shared sessions; skipping this launch.")
		return
	}
	defer lock.Release()

	neutralRoot := sharedSessionsStore(appData)

	// Always share the patched instance; share the official install too if present.
	installRoots := []string{filepath.Join(appData, "Claude-"+instanceName)}
	officialRoot := filepath.Join(appData, "Claude")
	if info, err := os.Stat(officialRoot); err == nil && info.IsDir() {
		installRoots = append(installRoots, officialRoot)
	}

	fmt.Println("Setting up shared Code/Cowork sessions...")
	for _, folder := range sharedSessionFolders {
		canonical := filepath.Join(neutralRoot, folder)
		if err := os.MkdirAll(canonical, 0755); err != nil {
			fmt.Printf("Warning: could not create shared store %s: %v\n", canonical, err)
			continue
		}
		for _, root := range installRoots {
			link := filepath.Join(root, folder)
			if err := ensureJunction(link, canonical); err != nil {
				fmt.Printf("Warning: could not share %s: %v\n", link, err)
			}
		}
	}
}

// RepairSessionSharing undoes the global session pooling created by older builds, which
// junctioned every instance's Cowork/Code session folders (and the official install's) into
// one shared store regardless of instance. It restores the launching named instance's folders
// to real directories, and — only when the default instance is not actively sharing — the
// official install's folders too (otherwise those junctions are legitimate). The default
// instance itself is left alone, since it is meant to be junctioned.
func RepairSessionSharing(instanceName string) {
	if instanceName == mainInstance {
		return
	}

	appData := os.Getenv("APPDATA")
	if appData == "" {
		return
	}

	lock, locked := utils.AcquirePatchLock(sessionLockName, sessionLockTimeout)
	if !locked {
		fmt.Println("Warning: timed out waiting to repair session folders; skipping this launch.")
		return
	}
	defer lock.Release()

	restoredAny := false

	// Restore this instance's own folders to real, isolated directories.
	instanceRoot := filepath.Join(appData, "Claude-"+instanceName)
	for _, folder := range sharedSessionFolders {
		if restoreRealFolder(filepath.Join(instanceRoot, folder)) {
			restoredAny = true
		}
	}

	// The official install's junctions are orphaned leftovers unless the default instance is
	// currently sharing with it; in that case they're correct, so leave them.
	if !defaultInstanceIsSharing(appData) {
		for _, folder := range sharedSessionFolders {
			if restoreRealFolder(filepath.Join(appData, "Claude", folder)) {
				restoredAny = true
			}
		}
	}

	if restoredAny {
		fmt.Printf("Restored isolated Cowork/Code sessions for instance %q.\n", instanceName)
	}
}

// restoreRealFolder converts link back into a real directory if it is currently a junction
// left by older global session-sharing: it removes the reparse point and moves the
// <link>.presync-backup folder back into place if present. Returns true if link was a junction
// that we undid. It never deletes the shared store the junction pointed at.
func restoreRealFolder(link string) bool {
	if _, err := os.Readlink(link); err != nil {
		return false // not a junction (or doesn't exist) — leave it alone
	}
	// os.Remove on a directory junction removes only the reparse point, not the target.
	if err := os.Remove(link); err != nil {
		fmt.Printf("Warning: could not remove session junction %s: %v\n", link, err)
		return false
	}
	backup := link + ".presync-backup"
	if _, err := os.Stat(backup); err == nil {
		if err := os.Rename(backup, link); err != nil {
			fmt.Printf("Warning: could not restore %s from backup: %v\n", link, err)
		}
	}
	// No backup: the folder had no pre-existing data; Claude recreates it as needed.
	return true
}

// defaultInstanceIsSharing reports whether the default instance is currently set up to share
// with the official install (its Cowork session folder is a junction).
func defaultInstanceIsSharing(appData string) bool {
	link := filepath.Join(appData, "Claude-"+mainInstance, sharedSessionFolders[0])
	_, err := os.Readlink(link)
	return err == nil
}

// CleanupOfficialJunctions removes the session junctions under %APPDATA%\Claude (the link
// only, never the target). Called before uninstalling the official MSIX so the uninstaller
// can't follow a junction into the canonical store and delete the shared sessions.
func CleanupOfficialJunctions() {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return
	}
	officialRoot := filepath.Join(appData, "Claude")
	for _, folder := range sharedSessionFolders {
		link := filepath.Join(officialRoot, folder)
		if _, err := os.Readlink(link); err != nil {
			continue // not a junction (or doesn't exist) — leave it alone
		}
		// os.Remove on a directory junction removes only the reparse point.
		if err := os.Remove(link); err != nil {
			fmt.Printf("Warning: could not remove junction %s: %v\n", link, err)
		}
	}
}

// ensureJunction makes link a directory junction pointing at canonical, idempotently.
// If link is already the correct junction it's a no-op; if it's a stale junction it's
// recreated; if it's a real directory its contents are merged into canonical, backed up,
// and replaced by the junction.
func ensureJunction(link, canonical string) error {
	// Already a reparse point we can resolve?
	if target, err := os.Readlink(link); err == nil {
		if samePath(target, canonical) {
			return nil
		}
		if err := os.Remove(link); err != nil {
			return fmt.Errorf("remove stale junction: %w", err)
		}
		return makeJunction(link, canonical)
	}

	info, err := os.Lstat(link)
	if err != nil {
		// Doesn't exist — just create the junction.
		return makeJunction(link, canonical)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		// A reparse point we couldn't read via Readlink; recreate it.
		os.Remove(link)
		return makeJunction(link, canonical)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", link)
	}

	// Real directory with data: merge into the canonical store first.
	if err := mergeTree(link, canonical); err != nil {
		return fmt.Errorf("merge into shared store: %w", err)
	}
	// Back up the original once; if a backup already exists the data is safely in the
	// canonical store, so just drop the real folder.
	backup := link + ".presync-backup"
	if _, err := os.Stat(backup); err == nil {
		if err := os.RemoveAll(link); err != nil {
			return fmt.Errorf("remove merged folder: %w", err)
		}
	} else if err := os.Rename(link, backup); err != nil {
		return fmt.Errorf("back up original folder: %w", err)
	}
	return makeJunction(link, canonical)
}

// makeJunction creates a directory junction (link -> target) via mklink /J, which needs
// no admin privileges. The link's parent is created if missing.
func makeJunction(link, target string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		return err
	}
	out, err := utils.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %q %q failed: %v\n%s", link, target, err, out)
	}
	return nil
}

// mergeTree recursively copies src into dst, overwriting a destination file only when the
// source file's modification time is newer (so a re-run never clobbers fresher data).
// File mtimes are preserved so the newer-wins comparison stays stable across runs.
func mergeTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		srcInfo, err := d.Info()
		if err != nil {
			return err
		}
		if dstInfo, err := os.Stat(target); err == nil {
			if !srcInfo.ModTime().After(dstInfo.ModTime()) {
				return nil // destination is same-or-newer — keep it
			}
		}
		return copyFile(path, target, srcInfo.ModTime())
	})
}

// copyFile copies src to dst (creating parent dirs) and restores src's modification time.
func copyFile(src, dst string, mtime time.Time) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, mtime, mtime)
}

// samePath reports whether two paths refer to the same location (case-insensitive on
// Windows, after cleaning and resolving to absolute form).
func samePath(a, b string) bool {
	ca, err1 := filepath.Abs(a)
	cb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(ca, cb)
}

// sharedSessionsStore is where the shared sessions really live (see SetupSessionSharing).
func sharedSessionsStore(appData string) string {
	return filepath.Join(appData, "ClaudeWebExtLauncher", "shared-sessions")
}

// sessionsShared reports whether there's a shared sessions store to undo.
func sessionsShared() bool {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return false
	}
	_, err := os.Stat(sharedSessionsStore(appData))
	return err == nil
}

// unshareSessions undoes SetupSessionSharing for good (uninstall): every Claude folder
// linked to the shared store (the official Claude's, and the instances') gets a real
// folder again, holding a copy of the sessions, and then the store is removed. Nothing
// is deleted while anything still links to it, so the official Claude keeps its
// sessions. With copyIntoInstances false (their data is about to be deleted) the
// instances' links are just removed, not filled.
func unshareSessions(instances []string, copyIntoInstances bool) error {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return nil
	}
	lock, locked := utils.AcquirePatchLock(sessionLockName, sessionLockTimeout)
	if !locked {
		return fmt.Errorf("another launcher is changing the shared sessions")
	}
	defer lock.Release()

	for _, folder := range sharedSessionFolders {
		if err := unshareFolder(filepath.Join(appData, "Claude", folder), true); err != nil {
			return err
		}
		for _, name := range instances {
			if err := unshareFolder(filepath.Join(claudeUserDataDir(name), folder), copyIntoInstances); err != nil {
				return err
			}
		}
	}
	if err := os.RemoveAll(sharedSessionsStore(appData)); err != nil {
		return fmt.Errorf("removing the shared sessions store: %w", err)
	}
	return nil
}

// unshareFolder replaces the junction link (if it is one) with a real folder, holding
// a copy of what it pointed at if fill. Removing a junction removes only the link,
// never its target.
func unshareFolder(link string, fill bool) error {
	target, err := os.Readlink(link)
	if err != nil {
		return nil // not a link (or not there): nothing to undo
	}
	fmt.Printf("Unsharing %s (was linked to %s)\n", link, target)
	if err := os.Remove(link); err != nil {
		return fmt.Errorf("removing the link %s: %w", link, err)
	}
	if _, err := os.Stat(target); err != nil || !fill {
		return os.MkdirAll(link, 0755)
	}
	if err := mergeTree(target, link); err != nil {
		return fmt.Errorf("copying the sessions into %s: %w", link, err)
	}
	return nil
}

// safeToDelete reports whether dir holds no session junction, which RemoveAll could
// otherwise follow into the shared store.
func safeToDelete(dir string) bool {
	for _, folder := range sharedSessionFolders {
		if _, err := os.Readlink(filepath.Join(dir, folder)); err == nil {
			return false
		}
	}
	return true
}
