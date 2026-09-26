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
	want := `exec '/usr/bin/open' '-n' '-a' '` + strings.ReplaceAll(home, "'", `'\''`) + `/Applications/Claude_WebExtension_Launcher.app' '--args' '--instance=work one'`
	if !strings.Contains(script, want) {
		t.Errorf("script:\n%s\nwant a line:\n%s", script, want)
	}
	// The script must run the command it shows: check sh parses it back to the same args.
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
	if got := agentLabel("work one"); got != "com.lugia19.claudewebextlauncher.login.work_20one" {
		t.Errorf("agentLabel = %q", got)
	}
	if got := agentLabel(launcherEntry); got != "com.lugia19.claudewebextlauncher.login" {
		t.Errorf("agentLabel(launcher) = %q", got)
	}
}

func TestEntriesRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if menuEntrySupported(launcherEntry) || hasMenuEntry(launcherEntry) {
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
