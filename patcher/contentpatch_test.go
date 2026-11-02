package patcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentPatchesValidateRequiredBehavior(t *testing.T) {
	for _, c := range []struct {
		name, source string
		wantError    bool
	}{
		{"supported bundle", `x=new Set(["devtools:","file:","app:"]);`, false},
		{"already supported upstream", "x=new Set([`devtools:`,`file:`,`chrome-extension:`]);", false},
		{"unknown bundle layout", `differentImplementation(); const unrelated="chrome-extension:";`, true},
		{"no bundle", "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, ".vite", "build", "index-abcd.js")
			if c.source != "" {
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(c.source), 0644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := applyContentPatches(root, supportedVersions["generic"])
			if c.wantError {
				if err == nil || !strings.Contains(err.Error(), "extension protocol support") {
					t.Fatalf("unsupported bundle was not diagnosed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(file)
			if err != nil || !strings.Contains(string(got), "chrome-extension:") {
				t.Fatalf("extension protocol missing from installed bundle: %s, %v", got, err)
			}
		})
	}
}

func TestContentPatchesDoNotWriteFailedPostcondition(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "bundle.js")
	if err := os.WriteFile(file, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := applyContentPatches(root, []Patch{{
		Name: "required behavior", Required: true, Files: []string{"bundle.js"},
		Func:   func(b []byte) ([]byte, bool) { return []byte("incomplete change"), true },
		Verify: func(b []byte) bool { return strings.Contains(string(b), "required behavior") },
	}})
	if err == nil {
		t.Fatal("unverified patch succeeded")
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "original" {
		t.Fatalf("unverified content was written: %q, %v", got, err)
	}
}
