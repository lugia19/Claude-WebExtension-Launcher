package main

// --uninstall: after asking, removes everything the launcher put on the system, in an
// order that never loses data: the Cowork/Code sessions shared with the official
// Claude are turned back into real folders first (Windows), then the patched Claude
// (elevated, through the worker), the shortcuts and registrations, the instances'
// data if asked, and finally the launcher's own files. The per-OS parts are in
// uninstall_<os>.go.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"claude-webext-patcher/gui"
	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
)

const (
	rowUnshare       = "unshare"
	rowRemoveClaude  = status.StepRemoveClaude // also reported by the worker on Windows
	rowUnregister    = "unregister"
	rowInstanceData  = "instance-data"
	rowLauncherFiles = "launcher-files"
)

func uninstallRows() []gui.Row {
	var rows []gui.Row
	if sessionsShared() {
		rows = append(rows, gui.Row{ID: rowUnshare, Label: "Shared Cowork and Code sessions"})
	}
	return append(rows,
		gui.Row{ID: rowRemoveClaude, Label: "Patched Claude"},
		gui.Row{ID: rowUnregister, Label: "Shortcuts and registrations"},
		gui.Row{ID: rowInstanceData, Label: "Instances' data"},
		gui.Row{ID: rowLauncherFiles, Label: "Launcher files"},
	)
}

// runUninstall is --uninstall. It returns the process exit code.
func runUninstall(debug bool) int {
	// The log goes to the temp folder: the launcher's own folder is removed.
	logPath := filepath.Join(os.TempDir(), "claude-webext-uninstall.log")
	var console io.Writer
	if debug {
		ensureConsole()
		console = os.Stdout
	}
	stop, _ := utils.StartLog(logPath, true, console)
	defer stop()
	fmt.Printf("Claude WebExtension Launcher %s: uninstall, %s\n", Version, time.Now().Format(time.RFC1123))

	// Asked through ui like any other question: in the window or the terminal. If the
	// window can't open, the answer is -1, which cancels.
	uninstalled := false
	work := func() error {
		if ui.Ask("Uninstall Claude Desktop (Extended)?",
			"This removes the launcher, the patched Claude and their shortcuts. The official Claude app, if you have it, isn't touched.",
			[]string{"Uninstall", "Cancel"}) != 0 {
			return nil
		}
		choice := ui.Ask("Also delete your instances' data?",
			"Their logins, settings and local sessions. Keep it if you might reinstall.",
			[]string{"Keep it", "Delete it", "Cancel"})
		if choice != 0 && choice != 1 {
			return nil
		}
		if err := uninstallAll(choice == 1, logPath); err != nil {
			return err
		}
		uninstalled = true
		return nil
	}

	var err error
	if debug {
		err = work()
	} else {
		err = gui.Run(gui.Options{
			Title:   "Uninstall Claude Desktop (Extended)",
			Rows:    uninstallRows(),
			LogPath: logPath,
			Done: func() string {
				if uninstalled {
					return "Claude Desktop (Extended) has been uninstalled."
				}
				return "" // cancelled: just close
			},
		}, func(s *gui.Status) error {
			ui = s
			return work()
		})
	}
	switch {
	case err != nil:
		fmt.Printf("Uninstall failed: %v\n", err)
		return 1
	case !uninstalled:
		fmt.Println("Cancelled.")
		return 0
	}
	fmt.Println("Uninstalled.")
	stop()
	finishUninstall()
	return 0
}

// uninstallAll does the removal, mirroring each step to the checklist.
func uninstallAll(deleteData bool, logPath string) error {
	instances := knownInstances()
	var open []string
	for _, name := range instances {
		if instanceRunning(name) {
			open = append(open, name)
		}
	}
	if len(open) > 0 {
		return fmt.Errorf("close Claude first (%s is open), then try again", strings.Join(open, ", "))
	}
	if otherLauncherRunning() {
		return fmt.Errorf("another launcher window is open; close it first, then try again")
	}

	if sessionsShared() {
		err := runStep(rowUnshare, func() (string, error) { return "", unshareSessions(instances, !deleteData) })
		if err != nil {
			return fmt.Errorf("couldn't give the shared sessions back their own folders: %w", err)
		}
	}
	if err := runStep(rowRemoveClaude, func() (string, error) { return removePatchedClaude(logPath) }); err != nil {
		return err
	}
	runStep(rowUnregister, func() (string, error) { return "", asWarning(removeRegistrations()) })
	runStep(rowInstanceData, func() (string, error) {
		if !deleteData {
			return "", stepSkipped("kept")
		}
		return "", asWarning(removeInstanceData(instances))
	})
	return runStep(rowLauncherFiles, removeLauncherFiles)
}

// stepSkipped, returned by a step, marks its row skipped, with the text as its note.
type stepSkipped string

func (s stepSkipped) Error() string { return string(s) }

// stepWarning, returned by a step, marks its row with a warning; the uninstall goes on.
type stepWarning struct{ error }

func asWarning(err error) error {
	if err == nil {
		return nil
	}
	return stepWarning{err}
}

// runStep shows row running, runs step, and shows how it went: done (with the note it
// returned), skipped, a warning, or failed. Only a failure is returned.
func runStep(row string, step func() (note string, err error)) error {
	ui.SetRow(row, status.Running, "", "")
	note, err := step()
	var skipped stepSkipped
	var warning stepWarning
	switch {
	case err == nil:
		ui.SetRow(row, status.Done, "", note)
	case errors.As(err, &skipped):
		ui.SetRow(row, status.Skipped, "", string(skipped))
	case errors.As(err, &warning):
		fmt.Printf("Warning: %v\n", warning.error)
		ui.SetRow(row, status.Warning, "", warning.Error())
	default:
		ui.SetRow(row, status.Failed, "", err.Error())
		return err
	}
	return nil
}

// knownInstances are all the instances the launcher may have created: the main one
// (under its current and old names), the listed ones, and any other instance data
// folder found.
func knownInstances() []string {
	found := findInstanceFolders(filepath.Dir(claudeUserDataDir(mainInstanceName)))
	return mergeInstanceNames([]string{mainInstanceName, legacyMainInstanceName}, utils.LoadSettings().Instances, found)
}

// mergeInstanceNames joins lists of instance names, without duplicates (compared
// ignoring case, as Windows and macOS folders are).
func mergeInstanceNames(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, name := range list {
			if key := strings.ToLower(name); name != "" && !seen[key] {
				seen[key] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// removeInstanceData deletes the instances' data folders (and the -3p folders next to
// them). On Windows the shared sessions have been unshared by now; a folder that still
// holds a junction is skipped rather than deleted through it.
func removeInstanceData(instances []string) error {
	var failed []string
	for _, name := range instances {
		dir := claudeUserDataDir(name)
		for _, d := range []string{dir, dir + companionSuffix} {
			if !dirExists(d) {
				continue
			}
			if !safeToDelete(d) {
				failed = append(failed, d+" (still linked to shared sessions)")
				continue
			}
			fmt.Printf("Deleting %s\n", d)
			if err := os.RemoveAll(d); err != nil {
				failed = append(failed, d)
			}
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("couldn't delete %s", strings.Join(failed, ", "))
	}
	return nil
}

// removeMimeHandler removes desktop from the x-scheme-handler/claude associations in a
// mimeapps.list, dropping the line if nothing else is left on it.
func removeMimeHandler(content, desktop string) string {
	const key = "x-scheme-handler/claude="
	lines := strings.Split(content, "\n")
	out := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key) {
			var kept []string
			for _, v := range strings.Split(strings.TrimPrefix(strings.TrimSpace(line), key), ";") {
				if v != "" && v != desktop {
					kept = append(kept, v)
				}
			}
			if len(kept) == 0 {
				continue
			}
			line = key + strings.Join(kept, ";") + ";"
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// startUninstall starts the installed launcher with --uninstall (the settings screen's
// Uninstall button), so the uninstall never runs inside a launch in progress.
func startUninstall() {
	exe := launcherBinary(installedLauncher())
	if _, err := os.Stat(exe); err != nil {
		exe, _ = os.Executable()
	}
	if err := exec.Command(exe, "--uninstall").Start(); err != nil {
		fmt.Printf("Could not start the uninstaller: %v\n", err)
	}
}

// otherLauncherRunning reports whether another launcher process is running. The one
// that started this uninstall (the Uninstall button) is on its way out, so it's given
// a moment.
func otherLauncherRunning() bool {
	for i := 0; i < 10; i++ {
		if !launcherProcessRunning() {
			return false
		}
		time.Sleep(300 * time.Millisecond)
	}
	return true
}
