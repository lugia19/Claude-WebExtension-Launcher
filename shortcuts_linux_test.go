package main

import "testing"

func TestEntryFileUnique(t *testing.T) {
	names := []string{defaultInstanceName, "work", "work one", "work_one", "work_20one", "wörk", "a/b", "a_2fb"}
	seen := map[string]string{}
	for _, name := range names {
		file := entryFile(name)
		if other, dup := seen[file]; dup {
			t.Errorf("%q and %q both map to %q", name, other, file)
		}
		seen[file] = name
	}
	if got := entryFile("work"); got != "claude-webext-launcher-work.desktop" {
		t.Errorf("entryFile(work) = %q", got)
	}
	if got := entryFile("work one"); got != "claude-webext-launcher-work_20one.desktop" {
		t.Errorf("entryFile(work one) = %q", got)
	}
}
