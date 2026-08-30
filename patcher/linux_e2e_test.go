package patcher

import (
	"claude-webext-patcher/asar"
	"embed"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The real binary wires patcher.EmbeddedFS from the main package (main.go).
// Under `go test` this package-level var starts empty, so re-embed just the
// generic wrapper.js (a tracked copy at patcher/resources/injections/generic/)
// so installWrapper can run. replaceIcons tolerates the missing icons dir.
//
//go:embed resources/injections/generic/wrapper.js
var testEmbeddedFS embed.FS

func init() {
	EmbeddedFS = testEmbeddedFS
}

// TestLinuxApplyPatchesEndToEnd exercises the full content-patching path against
// a working copy of the official Linux app tree (no download): it repacks the
// asar and verifies wrapper.js is installed, package.json main is redirected,
// and the protocol array gained chrome-extension: in the Linux backtick form.
// It only runs on Linux.
func TestLinuxApplyPatchesEndToEnd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only end-to-end patch test")
	}
	tree := os.Getenv("LINUX_CLAUDE_TREE")
	if tree == "" {
		t.Skip("LINUX_CLAUDE_TREE not set; skipping live patch test")
	}
	resourcesDir := filepath.Join(tree, "resources")
	if _, err := os.Stat(filepath.Join(resourcesDir, "app.asar")); err != nil {
		t.Fatalf("working copy missing app.asar: %v", err)
	}

	// Point globals at the working copy (the test mutates it in place).
	setAppPaths(tree)

	if err := applyPatches("1.40609.0"); err != nil {
		t.Fatalf("applyPatches failed: %v", err)
	}

	// The patched asar lives at resources/app.asar now.
	patchedAsar := filepath.Join(resourcesDir, "app.asar")
	if _, err := os.Stat(patchedAsar); err != nil {
		t.Fatalf("expected repacked asar: %v", err)
	}

	// Extract and inspect.
	extractDir := t.TempDir()
	if err := asar.Extract(patchedAsar, extractDir); err != nil {
		t.Fatalf("extracting patched asar: %v", err)
	}

	pkgData, err := os.ReadFile(filepath.Join(extractDir, "package.json"))
	if err != nil {
		t.Fatalf("reading package.json: %v", err)
	}
	if !strings.Contains(string(pkgData), `.vite/build/wrapper.js`) {
		t.Fatalf("package.json main not redirected: %s", pkgData)
	}

	if _, err := os.Stat(filepath.Join(extractDir, ".vite", "build", "wrapper.js")); err != nil {
		t.Fatalf("wrapper.js not installed: %v", err)
	}

	// Verify the protocol array was patched in the Linux backtick form.
	found := false
	err = filepath.Walk(filepath.Join(extractDir, ".vite", "build"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		name := filepath.Base(p)
		if !strings.HasPrefix(name, "index.") || !strings.HasSuffix(name, ".js") {
			return nil
		}
		if strings.Contains(name, "pre") || name == "wrapper.js" {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "`chrome-extension:`") {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking patched chunks: %v", err)
	}
	if !found {
		t.Fatal("chrome-extension: backtick entry not found in any chunk")
	}
}
