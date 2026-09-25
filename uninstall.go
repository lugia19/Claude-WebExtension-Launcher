package main

// --uninstall: after asking, removes everything the launcher put on the system, in an
// order that never loses data: the Cowork/Code sessions shared with the official
// Claude are turned back into real folders first (Windows), then the patched Claude
// (elevated, through the worker), the shortcuts and registrations, the instances'
// data if asked, and finally the launcher's own files. The per-OS parts are in
// uninstall_<os>.go.

import (
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
	rowRemoveClaude  = status.StepRemoveClaude // reported by the worker on Windows
	rowUnregister    = "unregister"
	rowInstanceData  = "instance-data"
	rowLauncherFiles = "launcher-files"
)

func uninstallRows() []gui.Row {
	var rows []gui.Row
	if hasSharedSessions {
		rows = append(rows, gui.Row{ID: rowUnshare, Label: "Shared Cowork and Code sessions"})
	}
	return append(rows,
		gui.Row{ID: rowRemoveClaude, Label: "Patched Claude"},
		gui.Row{ID: rowUnregister, Label: "Shortcuts and registrations"},
		gui.Row{ID: rowInstanceData, Label: "Instances' data"},
		gui.Row{ID: rowLauncherFiles, Label: "Launcher files"},
	)
}

const deleteDataLabel = "Also delete my instances' data (logins, settings and local sessions)"

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

	deleteData, ran := false, false
	work := func() error {
		ran = true
		return uninstallAll(deleteData, logPath)
	}
	var err error
	if debug {
		if ui.Ask("Uninstall Claude Desktop (Extended)?", uninstallSummary, []string{"Uninstall", "Cancel"}) != 0 {
			fmt.Println("Cancelled.")
			return 0
		}
		deleteData = ui.Ask(deleteDataLabel+"?", "", []string{"Keep it", "Delete it"}) == 1
		err = work()
	} else {
		err = gui.Run(gui.Options{
			Title:   "Uninstall Claude Desktop (Extended)",
			Rows:    uninstallRows(),
			LogPath: logPath,
			Setup: &gui.Setup{
				Title:    "Uninstall Claude Desktop (Extended)?",
				Subtitle: strings.Split(uninstallSummary, "\n"),
				Options:  []gui.SetupOption{{Label: deleteDataLabel}},
				Apply:    func(checked []bool) { deleteData = checked[0] },
				Confirm:  "Uninstall",
				Danger:   true,
			},
			SetupRequired: true,
			Cancel:        "Cancel",
			Done:          "Claude Desktop (Extended) has been uninstalled.",
		}, func(s *gui.Status) error {
			ui = s
			return work()
		})
	}
	if err != nil {
		fmt.Printf("Uninstall failed: %v\n", err)
		return 1
	}
	if !ran {
		fmt.Println("Cancelled.")
		return 0
	}
	fmt.Println("Uninstalled.")
	stop()
	finishUninstall()
	return 0
}

const uninstallSummary = "This removes the launcher, the patched Claude and their shortcuts.\n" +
	"The official Claude app, if you have it, isn't touched."

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

	if hasSharedSessions {
		ui.SetRow(rowUnshare, status.Running, "", "")
		if err := unshareSessions(instances); err != nil {
			ui.SetRow(rowUnshare, status.Failed, "", err.Error())
			return fmt.Errorf("couldn't give the shared sessions back their own folders: %w", err)
		}
		ui.SetRow(rowUnshare, status.Done, "", "")
	}

	ui.SetRow(rowRemoveClaude, status.Running, "", "")
	if err := removePatchedClaude(logPath); err != nil {
		ui.SetRow(rowRemoveClaude, status.Failed, "", err.Error())
		return err
	}

	ui.SetRow(rowUnregister, status.Running, "", "")
	if err := removeRegistrations(); err != nil {
		fmt.Printf("Warning: %v\n", err)
		ui.SetRow(rowUnregister, status.Warning, "", err.Error())
	} else {
		ui.SetRow(rowUnregister, status.Done, "", "")
	}

	if deleteData {
		ui.SetRow(rowInstanceData, status.Running, "", "")
		if err := removeInstanceData(instances); err != nil {
			fmt.Printf("Warning: %v\n", err)
			ui.SetRow(rowInstanceData, status.Warning, "", err.Error())
		} else {
			ui.SetRow(rowInstanceData, status.Done, "", "")
		}
	} else {
		ui.SetRow(rowInstanceData, status.Skipped, "", "kept")
	}

	ui.SetRow(rowLauncherFiles, status.Running, "", "")
	if err := removeLauncherFiles(); err != nil {
		ui.SetRow(rowLauncherFiles, status.Failed, "", err.Error())
		return err
	}
	ui.SetRow(rowLauncherFiles, status.Done, "", "")
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
			if _, err := os.Stat(d); err != nil {
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
// a few seconds.
func otherLauncherRunning() bool {
	for i := 0; i < 25; i++ {
		if !launcherProcessRunning() {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
	return true
}
