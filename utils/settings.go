package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings are the launcher's own preferences, in settings.json next to the log.
// Things that exist on disk (menu and startup entries) are deliberately not stored:
// they're read from the files themselves, so the setup screen, the flags and manual
// deletion can never disagree with a stored copy.
type Settings struct {
	SetupDone bool `json:"setupDone"` // the first-run setup screen has been shown

	// ManageInstances shows the instance list after the checklist.
	ManageInstances bool `json:"manageInstances,omitempty"`
	// Instances are the named instances in the list (the default one is implicit).
	Instances []string `json:"instances,omitempty"`
	// InstancesImported: existing instance data folders were added to Instances once.
	InstancesImported bool `json:"instancesImported,omitempty"`
}

// settingsMu serializes UpdateSettings within this process (the setup screen and the
// instance list change settings from different goroutines).
var settingsMu sync.Mutex

func settingsPath() string {
	return filepath.Join(logDir(), "settings.json")
}

// LoadSettings reads settings.json; a missing or unreadable file gives the defaults.
func LoadSettings() Settings {
	var s Settings
	if data, err := os.ReadFile(settingsPath()); err == nil {
		json.Unmarshal(data, &s)
	}
	return s
}

// SaveSettings writes settings.json.
func SaveSettings(s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath()), 0755); err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), data, 0644)
}

// UpdateSettings loads the settings, lets change modify them, and saves the result.
func UpdateSettings(change func(s *Settings)) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := LoadSettings()
	change(&s)
	return SaveSettings(s)
}
