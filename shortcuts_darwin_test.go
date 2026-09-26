package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShimScriptQuoting(t *testing.T) {
	home := t.TempDir() + "/it's a home"
	t.Setenv("HOME", home)
	script := shimScript("work one")
	// sh must parse the script back to exactly the command: print its words instead.
	out, err := exec.Command("sh", "-c", strings.Replace(strings.TrimPrefix(script, "#!/bin/sh\n"), "exec ", `printf '%s\n' `, 1)).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Split(strings.TrimSpace(string(out)), "\n"), openCommand("work one"); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("sh sees %q, want %q", got, want)
	}
}

func TestPlistsEscaped(t *testing.T) {
	t.Setenv("HOME", "/Users/a&b")
	if doc := agentPlist(agentLabel("work"), "work"); strings.Contains(doc, "a&b") || !strings.Contains(doc, "/Users/a&amp;b/Applications/") {
		t.Errorf("the launcher path isn't escaped:\n%s", doc)
	}
}

func TestBundleIDPart(t *testing.T) {
	seen := map[string]string{}
	for _, name := range []string{"work", "work one", "work_one", "work-one", "work-20one", "Main"} {
		id := bundleIDPart(name)
		if strings.Trim(id, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-.") != "" {
			t.Errorf("bundleIDPart(%q) = %q has characters a bundle identifier can't", name, id)
		}
		if other, dup := seen[id]; dup {
			t.Errorf("%q and %q both map to %q", name, other, id)
		}
		seen[id] = name
	}
	if got := bundleIDPart("work one"); got != "work-20one" {
		t.Errorf("bundleIDPart(work one) = %q", got)
	}
}

func TestAgentLabels(t *testing.T) {
	if got := agentLabel("work one"); got != "com.lugia19.claudewebextlauncher.login.work_20one" {
		t.Errorf("agentLabel = %q", got)
	}
	if got := agentLabel(launcherEntry); got != "com.lugia19.claudewebextlauncher.login" {
		t.Errorf("agentLabel(launcher) = %q", got)
	}
}

func TestEntriesRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if launcherHasMenuEntry || hasMenuEntry(launcherEntry) {
		t.Error("the launcher has no menu entry on macOS")
	}

	if err := addMenuEntry("work"); err != nil {
		t.Fatal(err)
	}
	if !hasMenuEntry("work") {
		t.Fatal("menu entry not found after adding it")
	}
	if err := addMenuEntry("work"); err != nil { // rewriting an existing one
		t.Fatal(err)
	}
	if err := setStartup("work", true); err != nil {
		t.Fatal(err)
	}
	if err := setStartup(launcherEntry, true); err != nil {
		t.Fatal(err)
	}
	if !hasStartup("work") || !hasStartup(launcherEntry) {
		t.Fatal("startup entries not found after adding them")
	}

	// An unrelated app with an entry's name is left alone.
	foreign := menuApp("other")
	os.MkdirAll(filepath.Join(foreign, "Contents", "MacOS"), 0755)
	if hasMenuEntry("other") {
		t.Error("an app the launcher didn't make counts as its entry")
	}
	if addMenuEntry("other") == nil {
		t.Error("replaced an app the launcher didn't make")
	}

	if err := removeRegistrations(); err != nil {
		t.Fatal(err)
	}
	if hasMenuEntry("work") || hasStartup("work") || hasStartup(launcherEntry) {
		t.Error("entries left after removeRegistrations")
	}
	if !fileExists(foreign) {
		t.Error("removeRegistrations removed an app the launcher didn't make")
	}
}
