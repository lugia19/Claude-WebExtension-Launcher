package utils

import "testing"

func TestSettingsRoundTrip(t *testing.T) {
	// Point the per-user data folder at a temp dir on every OS.
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)  // Windows
	t.Setenv("XDG_DATA_HOME", dir) // Linux
	t.Setenv("HOME", dir)          // macOS

	if LoadSettings().SetupDone {
		t.Fatal("fresh settings should have SetupDone false")
	}
	if err := SaveSettings(Settings{SetupDone: true}); err != nil {
		t.Fatal(err)
	}
	if !LoadSettings().SetupDone {
		t.Fatal("SetupDone didn't survive a save/load")
	}

	err := UpdateSettings(func(s *Settings) {
		s.ManageInstances = true
		s.Instances = append(s.Instances, "work")
	})
	if err != nil {
		t.Fatal(err)
	}
	got := LoadSettings()
	if !got.SetupDone || !got.ManageInstances || len(got.Instances) != 1 || got.Instances[0] != "work" {
		t.Fatalf("UpdateSettings result = %+v", got)
	}
}
