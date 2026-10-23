// Package status is how the worker process reports its progress to the launcher: one
// JSON object per line, appended to a file both know the path of. A file rather than
// a pipe because on Windows the worker is started through UAC, which can't hand it
// the launcher's pipes.
package status

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"sync"
)

// Steps the worker reports.
const (
	StepPatch      = "patch"
	StepExtensions = "extensions"
	StepCowork     = "cowork"

	// The uninstall worker's step: removing the patched Claude (and its Cowork service).
	StepRemoveClaude = "remove-claude"
)

// States a step can be in.
const (
	Running = "running"
	Done    = "done"
	Skipped = "skipped" // nothing needed doing
	Warning = "warning" // failed, but not fatally
	Failed  = "failed"
)

// Event is one line of the status file.
type Event struct {
	Step   string `json:"step"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"` // error or warning text
}

// Writer appends events to a status file. A nil *Writer discards events, so the
// worker can report unconditionally.
type Writer struct {
	mu sync.Mutex
	f  *os.File
}

// Create opens path for appending (creating it if needed).
func Create(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f}, nil
}

// Report appends one event. Errors are ignored: status reporting must never be the
// reason the worker fails.
func (w *Writer) Report(step, state, detail string) {
	if w == nil {
		return
	}
	line, _ := json.Marshal(Event{Step: step, State: state, Detail: detail})
	w.mu.Lock()
	defer w.mu.Unlock()
	w.f.Write(append(line, '\n'))
}

// Close closes the file.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	return w.f.Close()
}

// Reader returns the events appended to a status file since the last Poll.
type Reader struct {
	path    string
	offset  int64
	partial []byte // an incomplete last line, finished by a later write
}

// NewReader reads path from the beginning. The file doesn't need to exist yet.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// Poll returns the complete events written since the previous call. Lines that
// aren't valid JSON are skipped.
func (r *Reader) Poll() []Event {
	f, err := os.Open(r.path)
	if err != nil {
		return nil // not created yet
	}
	defer f.Close()
	if _, err := f.Seek(r.offset, io.SeekStart); err != nil {
		return nil
	}
	data, _ := io.ReadAll(f)
	r.offset += int64(len(data))

	data = append(r.partial, data...)
	r.partial = nil
	var events []Event
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			r.partial = append([]byte(nil), data...)
			break
		}
		var e Event
		if json.Unmarshal(data[:i], &e) == nil && e.Step != "" {
			events = append(events, e)
		}
		data = data[i+1:]
	}
	return events
}
