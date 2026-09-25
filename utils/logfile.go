package utils

import (
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
)

// LogPath is where the launcher and worker write their log.
func LogPath() string {
	return filepath.Join(logDir(), "launcher.log")
}

// StartLog sends everything printed to stdout/stderr into the log file at path, and
// also to console if it's non-nil (--debug). With rotate, the previous log is kept as
// launcher.previous.log and a fresh one is started; otherwise the file is appended to
// (the worker adds to the launcher's log). Returns a function that flushes and
// restores stdout/stderr.
func StartLog(path string, rotate bool, console io.Writer) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return func() {}, err
	}
	if rotate {
		os.Rename(path, strings.TrimSuffix(path, ".log")+".previous.log")
		os.WriteFile(path, nil, 0644) // start empty (if the rename failed, e.g. file in use)
	}
	// Append-only, never truncating: on Windows a handle opened with O_TRUNC writes at
	// its own offset rather than appending, and overwrites what the worker (a second
	// process appending to the same file) has written in the meantime.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return func() {}, err
	}
	// The runtime writes crashes to the process's own stderr, which a Windows GUI build
	// doesn't have; send them to the log too, so a crash is never silent.
	debug.SetCrashOutput(file, debug.CrashOptions{})

	r, w, err := os.Pipe()
	if err != nil {
		file.Close()
		return func() {}, err
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w

	var dst io.Writer = file
	if console != nil {
		// Console errors are ignored: a failing copy must not stop the log, or the pipe
		// fills up and every print blocks.
		dst = io.MultiWriter(file, ignoreErrors{console})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		io.Copy(dst, r)
	}()

	return func() {
		os.Stdout, os.Stderr = origOut, origErr
		w.Close()
		<-done
		r.Close()
		file.Close()
	}, nil
}

type ignoreErrors struct{ w io.Writer }

func (i ignoreErrors) Write(p []byte) (int, error) {
	i.w.Write(p)
	return len(p), nil
}
