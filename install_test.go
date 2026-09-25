package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShouldReplace(t *testing.T) {
	differs := func() bool { return true }
	same := func() bool { return false }
	cases := []struct {
		name               string
		exists             bool
		running, installed string
		content            func() bool
		want               bool
	}{
		{"nothing installed", false, "3.3.3", "", same, true},
		{"newer", true, "3.4.0", "3.3.3", same, true},
		{"older", true, "3.3.2", "3.3.3", differs, false},
		{"same version, same build", true, "3.3.3", "3.3.3", same, false},
		{"same version, other build", true, "3.3.3", "3.3.3", differs, true},
		{"unknown installed version, other build", true, "3.3.3", "", differs, true},
		{"unknown installed version, same build", true, "3.3.3", "", same, false},
	}
	for _, c := range cases {
		if got := shouldReplace(c.exists, c.running, c.installed, c.content); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

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
	if err := replaceFile(src, dst, 0755); err != nil {
		t.Fatal(err)
	}
	if !sameFileContent(src, dst) {
		t.Fatal("first install didn't copy the file")
	}

	write(src, "v2")
	if err := replaceFile(src, dst, 0755); err != nil {
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

func TestSameLocationThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(real, "launcher")
	if err := os.WriteFile(file, []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("can't create symlinks here: %v", err)
	}
	if !sameLocation(file, filepath.Join(link, "launcher")) {
		t.Error("the same file through a symlinked folder wasn't recognized")
	}
	if sameLocation(file, filepath.Join(dir, "other")) {
		t.Error("different paths matched")
	}
}

func TestHandOffArgs(t *testing.T) {
	saved := os.Args
	defer func() { os.Args = saved }()
	os.Args = []string{"launcher", "--instance=work", "--installed-from=/old/path", "--debug"}
	got := handOffArgs("/new/from")
	want := []string{"--instance=work", "--debug", "--installed-from=/new/from"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
