package main

import (
	"reflect"
	"testing"
)

func TestMergeInstanceNames(t *testing.T) {
	got := mergeInstanceNames(
		[]string{mainInstanceName, legacyMainInstanceName},
		[]string{"work", "Main"},
		[]string{"WORK", "personal", ""},
	)
	want := []string{"Main", "modified", "work", "personal"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRemoveMimeHandler(t *testing.T) {
	const ours = "claude-webext-handler.desktop"
	in := "[Default Applications]\n" +
		"x-scheme-handler/claude=claude-webext-handler.desktop\n" +
		"text/html=firefox.desktop\n" +
		"[Added Associations]\n" +
		"x-scheme-handler/claude=com.anthropic.Claude.desktop;claude-webext-handler.desktop;\n"
	want := "[Default Applications]\n" +
		"text/html=firefox.desktop\n" +
		"[Added Associations]\n" +
		"x-scheme-handler/claude=com.anthropic.Claude.desktop;\n"
	if got := removeMimeHandler(in, ours); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if unchanged := "text/html=firefox.desktop\n"; removeMimeHandler(unchanged, ours) != unchanged {
		t.Error("a file without our handler was changed")
	}
}
