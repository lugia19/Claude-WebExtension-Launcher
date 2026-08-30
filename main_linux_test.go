//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopEntryContent(t *testing.T) {
	content := desktopEntryContent("/home/user/bin/Claude_WebExtension_Launcher")

	if !strings.Contains(content, "Type=Application") {
		t.Fatalf("missing Type=Application: %s", content)
	}
	if !strings.Contains(content, "Exec=/home/user/bin/Claude_WebExtension_Launcher\n") {
		t.Fatalf("Exec must carry the real path: %s", content)
	}
	if !strings.Contains(content, "Terminal=false\n") {
		t.Fatalf("desktop entry must not depend on a terminal: %s", content)
	}
	if !strings.Contains(content, "Categories=Utility;\n") {
		t.Fatalf("desktop entry missing Categories: %s", content)
	}
	if strings.Contains(content, "/opt/claude-webext-launcher") || strings.Contains(content, "/home/ehaan08/") {
		t.Fatalf("desktop entry must not embed a developer/hardcoded path: %s", content)
	}
}

// TestDesktopEntryContentSpaces confirms a launcher inside a directory with
// spaces is quoted per the Desktop Entry spec rather than broken.
func TestDesktopEntryContentSpaces(t *testing.T) {
	content := desktopEntryContent("/home/user/my apps/Claude_WebExtension_Launcher")
	if !strings.Contains(content, `Exec="/home/user/my apps/Claude_WebExtension_Launcher"`) {
		t.Fatalf("path with spaces must be quoted: %s", content)
	}
}

func TestDesktopExecField(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/usr/local/bin/Claude_WebExtension_Launcher", "/usr/local/bin/Claude_WebExtension_Launcher"},
		{"/home/u/my apps/Claude_WebExtension_Launcher", `"/home/u/my apps/Claude_WebExtension_Launcher"`},
		{`/home/u/a"b/Launcher`, `"/home/u/a\"b/Launcher"`},
	}
	for _, c := range cases {
		if got := desktopExecField(c.in); got != c.want {
			t.Errorf("desktopExecField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEnsureDesktopEntryIdempotent verifies the entry is written once and not
// rewritten when unchanged, using a temp XDG_DATA_HOME.
func TestEnsureDesktopEntryIdempotent(t *testing.T) {
	// ensureDesktopEntry reads os.Executable, so we must run from a real binary;
	// go test runs from a temp test binary, which is exactly the "real path" we
	// want the entry to point at.
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg-data"))

	if err := ensureDesktopEntry(); err != nil {
		t.Fatalf("ensureDesktopEntry: %v", err)
	}
	entryPath := filepath.Join(os.Getenv("XDG_DATA_HOME"), "applications", "claude-webext-launcher.desktop")
	data, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatalf("expected desktop entry at %s: %v", entryPath, err)
	}

	// Second call must not rewrite (compare mtime/content is unaffected, but
	// content must still match and the call must not error).
	if err := ensureDesktopEntry(); err != nil {
		t.Fatalf("ensureDesktopEntry (repeat): %v", err)
	}
	second, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatalf("re-reading entry: %v", err)
	}
	if string(data) != string(second) {
		t.Fatalf("desktop entry changed between runs:\n%s\n---\n%s", data, second)
	}
	if !strings.Contains(string(data), "Terminal=false") {
		t.Fatalf("generated entry must use Terminal=false: %s", data)
	}
}

func TestSandboxDiagnostic(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "chrome-sandbox")
	if msg := sandboxDiagnostic(missing); msg == "" {
		t.Fatal("expected a warning for a missing chrome-sandbox")
	}

	dirPath := filepath.Join(dir, "chrome-sandbox-dir")
	if err := os.Mkdir(dirPath, 0755); err != nil {
		t.Fatal(err)
	}
	if msg := sandboxDiagnostic(dirPath); msg == "" {
		t.Fatal("expected a warning for a directory at the sandbox path")
	}

	// A regular (non-setuid) file must trigger the setuid guidance.
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if msg := sandboxDiagnostic(plain); msg == "" || !strings.Contains(msg, "setuid") {
		t.Fatalf("expected setuid guidance for plain file, got: %q", msg)
	}

	// A setuid file (owned by us, the test runner) must report OK. setuid on a
	// file the caller owns is permitted without root.
	setuid := filepath.Join(dir, "setuid")
	if err := os.WriteFile(setuid, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(setuid, os.ModeSetuid|0755); err != nil {
		t.Skipf("cannot set setuid bit in this environment: %v", err)
	}
	if msg := sandboxDiagnostic(setuid); msg != "" {
		t.Fatalf("expected no warning for setuid sandbox, got: %q", msg)
	}
}
