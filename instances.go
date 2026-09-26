package main

// The instance list (shown after the checklist when "Manage multiple instances" is
// on): the named instances are kept in settings.json, the default one is implicit.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"claude-webext-patcher/gui"
	"claude-webext-patcher/utils"
)

const maxInstanceNameLength = 32

var instanceNameChars = regexp.MustCompile(`^[A-Za-z0-9 _-]+$`)

// instanceNameProblem says why name can't be a new instance next to existing, or ""
// if it can. Names become folder and shortcut names, hence the short character set.
// Folders are case-insensitive on Windows (and by default on macOS), so names that
// differ only in case count as the same.
func instanceNameProblem(name string, existing []string) string {
	switch {
	case name == "":
		return "Enter a name."
	case len(name) > maxInstanceNameLength:
		return fmt.Sprintf("Use at most %d characters.", maxInstanceNameLength)
	case !instanceNameChars.MatchString(name):
		return "Use only letters, digits, spaces, - and _."
	case strings.TrimSpace(name) != name:
		return "Names can't start or end with a space."
	case strings.EqualFold(name, mainInstanceName):
		return "There's already an instance called " + mainInstanceName + "."
	case strings.EqualFold(name, legacyMainInstanceName):
		return "That's the main instance's old name; pick another."
	}
	for _, e := range existing {
		if strings.EqualFold(name, e) {
			return "There's already an instance called " + e + "."
		}
	}
	return ""
}

// findInstanceFolders returns the names of the instances that have a data folder in
// appData ("Claude-<name>", as Claude's userData for --instance <name>). Only folders
// that look like Electron user data count, so other apps' "Claude-*" folders are left
// out, as are names that couldn't be added by hand.
func findInstanceFolders(appData string) []string {
	entries, err := os.ReadDir(appData)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		name, ok := strings.CutPrefix(e.Name(), "Claude-")
		if !ok || !e.IsDir() || instanceNameProblem(name, names) != "" {
			continue
		}
		dir := filepath.Join(appData, e.Name())
		if fileExists(filepath.Join(dir, "Preferences")) || fileExists(filepath.Join(dir, "Local State")) {
			names = append(names, name)
		}
	}
	return names
}

// listedInstances returns the named instances, first adding the ones that already
// have data folders (once: after that, the list is the user's).
func listedInstances() []string {
	s := utils.LoadSettings()
	if s.InstancesImported {
		return s.Instances
	}
	found := findInstanceFolders(filepath.Dir(claudeUserDataDir(mainInstanceName)))
	err := utils.UpdateSettings(func(s *utils.Settings) {
		for _, name := range found {
			if instanceNameProblem(name, s.Instances) == "" {
				s.Instances = append(s.Instances, name)
			}
		}
		s.InstancesImported = true
	})
	if err != nil {
		fmt.Printf("Warning: could not save the instance list: %v\n", err)
	}
	if len(found) > 0 {
		fmt.Printf("Added existing instances to the list: %s\n", strings.Join(found, ", "))
	}
	return utils.LoadSettings().Instances
}

// rememberInstance adds an instance started with --instance to the list, when the
// list is in use and the name is one that could have been added there.
func rememberInstance(name string) {
	if name == mainInstance || !utils.LoadSettings().ManageInstances {
		return
	}
	if instanceNameProblem(name, listedInstances()) != "" {
		return
	}
	utils.UpdateSettings(func(s *utils.Settings) {
		if instanceNameProblem(name, s.Instances) == "" {
			s.Instances = append(s.Instances, name)
		}
	})
}

// deleteInstance removes a named instance: its data folder, shortcuts and logs, and
// its place in the list.
func deleteInstance(name string) error {
	if instanceRunning(name) {
		return fmt.Errorf("%s is open. Close it first, then try again", name)
	}
	fmt.Printf("Deleting instance %q\n", name)
	// An older build could have pooled a named instance's sessions with the shared
	// store through junctions; undo that first, so nothing outside is touched.
	RepairSessionSharing(name)
	if hasMenuEntry(name) {
		if err := removeMenuEntry(name); err != nil {
			return fmt.Errorf("couldn't remove the menu entry: %w", err)
		}
	}
	if hasStartup(name) {
		if err := setStartup(name, false); err != nil {
			return fmt.Errorf("couldn't remove the startup entry: %w", err)
		}
	}
	if err := os.RemoveAll(claudeUserDataDir(name)); err != nil {
		return fmt.Errorf("couldn't delete its data folder: %w", err)
	}
	if err := os.RemoveAll(claudeUserDataDir(name) + companionSuffix); err != nil {
		return fmt.Errorf("couldn't delete its %s folder: %w", companionSuffix, err)
	}
	logPath := utils.LogPath(name, mainInstanceName)
	os.Remove(logPath)
	os.Remove(strings.TrimSuffix(logPath, ".log") + ".previous.log")
	return utils.UpdateSettings(func(s *utils.Settings) {
		kept := s.Instances[:0]
		for _, n := range s.Instances {
			if n != name {
				kept = append(kept, n)
			}
		}
		s.Instances = kept
	})
}

// instanceList is the instance list's view of the launcher. The main instance is
// listed as mainInstanceName, which is also what its shortcuts pass; launchable maps
// it to this run's name for it (the old one, if its folder isn't migrated yet).
func instanceList() *gui.Instances {
	return &gui.Instances{
		List: func() []gui.Instance {
			list := []gui.Instance{{Name: mainInstanceName, Display: mainInstanceName}}
			for _, name := range listedInstances() {
				list = append(list, gui.Instance{Name: name, Display: name, Deletable: true})
			}
			return list
		},
		Launch: func(name string) error { return launchInstance(launchable(name)) },
		Validate: func(name string) string {
			return instanceNameProblem(name, listedInstances())
		},
		Add: func(name string) error {
			return utils.UpdateSettings(func(s *utils.Settings) {
				if instanceNameProblem(name, s.Instances) == "" {
					s.Instances = append(s.Instances, name)
				}
			})
		},
		Running:          func(name string) bool { return instanceRunning(launchable(name)) },
		Delete:           deleteInstance,
		Settings:         instanceSettings,
		LauncherSettings: func() *gui.Setup { return launcherSettings("Launcher settings", launcherSettingsSubtitle) },
	}
}

var launcherSettingsSubtitle = []string{
	"The launcher's own shortcuts, and whether it shows this list.",
	"Turning the list off takes effect the next time the launcher starts.",
}

// launchable maps a listed instance name to the name to launch it by.
func launchable(name string) string {
	if name == mainInstanceName {
		return mainInstance
	}
	return name
}

// instanceSettings is an instance's settings screen: its own menu and startup entries,
// which launch it directly.
func instanceSettings(name string) *gui.Setup {
	return &gui.Setup{
		Title:    "Settings for " + name,
		Subtitle: []string{"Shortcuts that launch this instance directly, without the list."},
		Options: []gui.SetupOption{
			{Label: "Add to the applications menu", Checked: hasMenuEntry(name)},
			{Label: "Start when I log in", Checked: hasStartup(name)},
		},
		Apply: func(checked []bool) {
			applyShortcuts(name, checked[0], checked[1])
		},
	}
}
