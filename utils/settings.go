package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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

// SaveSettings writes settings.json. It's written to a temporary file and renamed into
// place, so a reader never sees half of it.
func SaveSettings(s Settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := settingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	// On Windows the rename fails while another process has the file open (reading
	// it); that's brief, so retry a few times.
	for attempt := 0; ; attempt++ {
		err = os.Rename(tmp, path)
		if err == nil || attempt == 4 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// UpdateSettings loads the settings, lets change modify them, and saves the result,
// with other goroutines and other launcher processes kept out in between (so two
// launchers adding instances at once don't lose one of them).
func UpdateSettings(change func(s *Settings)) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if lock, ok := AcquirePatchLock(SettingsLockName, 10*time.Second); ok {
		defer lock.Release()
	} // else another launcher is stuck holding it: go ahead rather than lose the change
	s := LoadSettings()
	change(&s)
	return SaveSettings(s)
}
