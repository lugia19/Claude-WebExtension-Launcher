package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReplaceFile copies src over dst without ever leaving dst half written: the copy is
// made next to dst, the old dst is moved aside, and the copy renamed into place.
// Moving a file aside works even while it's running (on Windows too, where it can't
// be overwritten or deleted then); the old copy is removed if possible, and otherwise
// left as dst + ".old" (or ".old-<n>", if an earlier one is still running too) for
// CleanupReplaced on a later start.
//
// restore puts the old copy back, e.g. when the new one turns out not to start. It
// only can while the old copy is still there: when it's running (as when a program
// replaces itself).
func ReplaceFile(src, dst string, mode os.FileMode) (restore func() error, err error) {
	staged, old := dst+".new", dst+".old"
	if err := copyWithMode(src, staged, mode); err != nil {
		os.Remove(staged)
		return nil, err
	}
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		// Still running (e.g. a launcher that never exited): leave it be.
		old = fmt.Sprintf("%s.old-%d", dst, time.Now().UnixNano())
	}
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			os.Remove(staged)
			return nil, fmt.Errorf("moving the old file aside: %w", err)
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		os.Rename(old, dst) // put the old one back
		return nil, fmt.Errorf("moving the new file in: %w", err)
	}
	os.Remove(old)
	return func() error {
		if _, err := os.Stat(old); err != nil {
			return fmt.Errorf("the old copy is gone: %w", err)
		}
		if err := os.Remove(dst); err != nil {
			return err
		}
		return os.Rename(old, dst)
	}, nil
}

// CleanupReplaced removes what ReplaceFile(_, dst) couldn't: old copies that were
// still running then, and a staged copy left by a failed one. Those still running
// now are left for next time.
func CleanupReplaced(dst string) {
	os.Remove(dst + ".new")
	entries, _ := os.ReadDir(filepath.Dir(dst))
	prefix := filepath.Base(dst) + ".old"
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			os.Remove(filepath.Join(filepath.Dir(dst), e.Name()))
		}
	}
}

func copyWithMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
