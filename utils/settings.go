package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings are the launcher's own preferences, in settings.json next to the log.
// Things that exist on disk (menu and startup entries) are deliberately not stored:
// they're read from the files themselves, so the setup screen, the flags and manual
// deletion can never disagree with a stored copy.
type Settings struct {
	SetupDone bool `json:"setupDone"` // the first-run setup screen has been shown
}

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
