package extensions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverInterruptedExtensionUpdate(t *testing.T) {
	for _, c := range []struct {
		name                            string
		current, backup, invalidCurrent bool
	}{
		{"partial download", true, false, false},
		{"old copy moved aside", false, true, false},
		{"new copy installed", true, true, false},
		{"invalid current copy keeps backup", true, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			parent := t.TempDir()
			destination := filepath.Join(parent, "test-extension")
			stage := filepath.Join(parent, ".test-extension-update-abandoned")
			previous := filepath.Join(stage, "previous")
			writeCopy := func(dir, version string) {
				t.Helper()
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(version), 0644); err != nil {
					t.Fatal(err)
				}
			}
			old := `{"manifest_version":3,"name":"Test","version":"1.0"}`
			current := `{"manifest_version":3,"name":"Test","version":"2.0"}`
			if c.invalidCurrent {
				current = "invalid"
			}
			if err := os.MkdirAll(stage, 0755); err != nil {
				t.Fatal(err)
			}
			if c.current {
				writeCopy(destination, current)
			}
			if c.backup {
				writeCopy(previous, old)
			}
			unrelated := filepath.Join(parent, ".other-extension-update-keep")
			if err := os.Mkdir(unrelated, 0755); err != nil {
				t.Fatal(err)
			}
			err := recoverExtensionUpdates(destination)
			if c.invalidCurrent {
				if err == nil {
					t.Fatal("invalid current copy was accepted")
				}
				if got, err := os.ReadFile(filepath.Join(previous, "manifest.json")); err != nil || string(got) != old {
					t.Fatalf("backup lost: %q, %v", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := current
			if !c.current {
				want = old
			}
			if got, err := os.ReadFile(filepath.Join(destination, "manifest.json")); err != nil || string(got) != want {
				t.Fatalf("installed copy: %q, %v", got, err)
			}
			if _, err := os.Stat(stage); !os.IsNotExist(err) {
				t.Fatalf("abandoned directory remains: %v", err)
			}
			if _, err := os.Stat(unrelated); err != nil {
				t.Fatalf("unrelated directory touched: %v", err)
			}
		})
	}
}
