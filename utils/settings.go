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
	// InstanceOptions are how each instance is launched, by the name it's launched with.
	InstanceOptions map[string]InstanceOptions `json:"instanceOptions,omitempty"`
}

// The remote debugging and Node inspector ports when none was chosen.
const (
	DefaultDebugPort     = 9222
	DefaultInspectorPort = 9229
)

// InstanceOptions are an instance's launch options.
type InstanceOptions struct {
	// RemoteDebugging opens Chromium's remote debugging port (DebugPort).
	RemoteDebugging bool `json:"remoteDebugging,omitempty"`
	DebugPort       int  `json:"debugPort,omitempty"` // 0: DefaultDebugPort
	// DevMode (advanced debug mode) makes Claude's checks for Anthropic's test harness
	// pass, which turns on its internal test features.
	DevMode bool `json:"devMode,omitempty"`
	// Env are KEY=value environment variables for Claude, set in advanced debug mode
	// (most of its features are configured through them).
	Env []string `json:"env,omitempty"`
	// Inspector starts Node's inspector in Claude's main process, on InspectorPort. It
	// needs advanced debug mode (version.dll turns on the --inspect fuse only then).
	Inspector     bool `json:"inspector,omitempty"`
	InspectorPort int  `json:"inspectorPort,omitempty"` // 0: DefaultInspectorPort
}

// Port is the remote debugging port to use.
func (o InstanceOptions) Port() int {
	if o.DebugPort == 0 {
		return DefaultDebugPort
	}
	return o.DebugPort
}

// NodeInspectorPort is the Node inspector port to use.
func (o InstanceOptions) NodeInspectorPort() int {
	if o.InspectorPort == 0 {
		return DefaultInspectorPort
	}
	return o.InspectorPort
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
	lock, ok := AcquirePatchLock(SettingsLockName, 10*time.Second)
	if !ok {
		// Going ahead unlocked could silently undo another launcher's change.
		return fmt.Errorf("the settings are being changed by another launcher; try again")
	}
	defer lock.Release()
	s := LoadSettings()
	change(&s)
	return SaveSettings(s)
}
