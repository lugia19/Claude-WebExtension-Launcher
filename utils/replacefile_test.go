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
