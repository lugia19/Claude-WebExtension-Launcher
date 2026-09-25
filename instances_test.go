package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInstanceNameProblem(t *testing.T) {
	existing := []string{"work", "Side Project"}
	ok := []string{"personal", "work2", "my_work", "team-a", "A b C", strings.Repeat("x", maxInstanceNameLength)}
	for _, name := range ok {
		if p := instanceNameProblem(name, existing); p != "" {
			t.Errorf("%q rejected: %s", name, p)
		}
	}
	bad := []string{
		"", // empty
		strings.Repeat("x", maxInstanceNameLength+1), // too long
		"a/b", "a\\b", "a:b", "a.b", "wörk", // characters outside the set
		" work", "work ", // leading/trailing space
		"Main", "main", "modified", "Modified", // the main instance, and its old name
		"WORK", "side project", // duplicates, ignoring case
	}
	for _, name := range bad {
		if instanceNameProblem(name, existing) == "" {
			t.Errorf("%q accepted", name)
		}
	}
}

func TestFindInstanceFolders(t *testing.T) {
	appData := t.TempDir()
	mk := func(dir, file string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(appData, dir), 0755); err != nil {
			t.Fatal(err)
		}
		if file != "" {
			if err := os.WriteFile(filepath.Join(appData, dir, file), nil, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("Claude-work", "Preferences")         // an instance
	mk("Claude-side project", "Local State") // an instance, with a space
	mk("Claude-Main", "Preferences")         // the main instance: always listed anyway
	mk("Claude-modified", "Preferences")     // its old folder, before the rename
	mk("Claude-a.b", "Preferences")          // a name that couldn't be added by hand
	mk("Claude-helper", "")                  // not Electron user data
	mk("Claude", "Preferences")              // the official install
	mk("Other-app", "Preferences")
	if err := os.WriteFile(filepath.Join(appData, "Claude-file"), nil, 0644); err != nil { // not a folder
		t.Fatal(err)
	}

	got := findInstanceFolders(appData)
	want := []string{"side project", "work"} // ReadDir order is sorted
	if !reflect.DeepEqual(got, want) {
		t.Errorf("findInstanceFolders = %q, want %q", got, want)
	}
}
