package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "installed")
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write(src, "v1")
	if err := ReplaceFile(src, dst, 0755); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "v1" {
		t.Fatalf("installed content = %q, want v1", got)
	}

	write(src, "v2")
	if err := ReplaceFile(src, dst, 0755); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "v2" {
		t.Fatalf("installed content = %q, want v2", got)
	}
	for _, leftover := range []string{dst + ".new", dst + ".old"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}
}

// An old copy still in use (on Windows: still running, or open) is left alone, and
// the replace still works; CleanupReplaced removes it once it's free.
func TestReplaceFileOldInUse(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "installed")
	for path, content := range map[string]string{src: "v2", dst: "v1", dst + ".old": "v0"} {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	busy, err := os.Open(dst + ".old")
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceFile(src, dst, 0755); err != nil {
		busy.Close()
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "v2" {
		t.Errorf("installed content = %q, want v2", got)
	}

	busy.Close()
	CleanupReplaced(dst)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "src" && e.Name() != "installed" {
			t.Errorf("%s left behind", e.Name())
		}
	}
}
