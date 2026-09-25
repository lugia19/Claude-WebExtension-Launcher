package status

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.jsonl")
	r := NewReader(path)
	if got := r.Poll(); len(got) != 0 {
		t.Fatalf("poll before the file exists = %v, want nothing", got)
	}

	w, err := Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	w.Report(StepPatch, Running, "")
	w.Report(StepPatch, Done, "")
	got := r.Poll()
	if len(got) != 2 || got[0] != (Event{StepPatch, Running, ""}) || got[1] != (Event{StepPatch, Done, ""}) {
		t.Fatalf("first poll = %v", got)
	}

	if got := r.Poll(); len(got) != 0 {
		t.Fatalf("second poll repeated events: %v", got)
	}

	w.Report(StepExtensions, Failed, "no network")
	got = r.Poll()
	if len(got) != 1 || got[0] != (Event{StepExtensions, Failed, "no network"}) {
		t.Fatalf("third poll = %v", got)
	}
}

func TestPartialLinesAndGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.jsonl")
	r := NewReader(path)

	// A line split across two writes, with a garbage line in between.
	os.WriteFile(path, []byte(`{"step":"patch","sta`), 0644)
	if got := r.Poll(); len(got) != 0 {
		t.Fatalf("incomplete line produced %v", got)
	}

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	f.Write([]byte("te\":\"running\"}\nnot json\n{\"state\":\"done\"}\n{\"step\":\"cowork\",\"state\":\"done\"}\n"))
	f.Close()

	got := r.Poll()
	want := []Event{{StepPatch, Running, ""}, {StepCowork, Done, ""}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v (garbage and step-less lines skipped)", got, want)
	}
}

func TestNilWriter(t *testing.T) {
	var w *Writer
	w.Report(StepPatch, Running, "") // must not panic
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
