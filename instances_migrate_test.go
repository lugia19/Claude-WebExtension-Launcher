package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFolder(t *testing.T) {
	notRunning := func() bool { return false }
	setup := func(t *testing.T, dirs ...string) (oldDir, newDir string) {
		t.Helper()
		base := t.TempDir()
		oldDir, newDir = filepath.Join(base, "Claude-modified"), filepath.Join(base, "Claude-Main")
		for _, d := range dirs {
			if err := os.MkdirAll(filepath.Join(base, d), 0755); err != nil {
				t.Fatal(err)
			}
		}
		os.WriteFile(filepath.Join(oldDir, "Preferences"), []byte("old"), 0644) // no-op without the folder
		return oldDir, newDir
	}

	t.Run("renames", func(t *testing.T) {
		oldDir, newDir := setup(t, "Claude-modified", "Claude-modified-3p")
		if got := migrateFolder(oldDir, newDir, notRunning); got != mainInstanceName {
			t.Fatalf("got %q", got)
		}
		if dirExists(oldDir) || !fileExists(filepath.Join(newDir, "Preferences")) {
			t.Fatal("folder wasn't moved with its contents")
		}
		if dirExists(oldDir+companionSuffix) || !dirExists(newDir+companionSuffix) {
			t.Fatal("the -3p companion folder wasn't moved")
		}
	})
	t.Run("nothing to migrate", func(t *testing.T) {
		oldDir, newDir := setup(t)
		if got := migrateFolder(oldDir, newDir, notRunning); got != mainInstanceName || dirExists(newDir) {
			t.Fatalf("got %q, new folder created: %v", got, dirExists(newDir))
		}
	})
	t.Run("both exist", func(t *testing.T) {
		oldDir, newDir := setup(t, "Claude-modified", "Claude-Main")
		if got := migrateFolder(oldDir, newDir, notRunning); got != mainInstanceName || !dirExists(oldDir) {
			t.Fatalf("got %q, old folder kept: %v", got, dirExists(oldDir))
		}
	})
	t.Run("old instance running", func(t *testing.T) {
		oldDir, newDir := setup(t, "Claude-modified")
		got := migrateFolder(oldDir, newDir, func() bool { return true })
		if got != legacyMainInstanceName || !dirExists(oldDir) || dirExists(newDir) {
			t.Fatalf("got %q; old %v new %v", got, dirExists(oldDir), dirExists(newDir))
		}
	})
}
