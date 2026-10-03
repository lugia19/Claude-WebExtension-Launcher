package utils

import (
	"fmt"
	"io"
	"os"
)

// ReplaceFile copies src over dst without ever leaving dst half written: the copy is
// made next to dst, the old dst is moved aside, and the copy renamed into place.
// Moving a file aside works even while it's running (on Windows too, where it can't
// be overwritten or deleted then); the old copy is removed if possible, and otherwise
// left as dst + ".old" for the caller to remove on a later start.
func ReplaceFile(src, dst string, mode os.FileMode) error {
	staged, old := dst+".new", dst+".old"
	if err := copyWithMode(src, staged, mode); err != nil {
		os.Remove(staged)
		return err
	}
	os.Remove(old)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			os.Remove(staged)
			return fmt.Errorf("moving the old file aside: %w", err)
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		os.Rename(old, dst) // put the old one back
		return fmt.Errorf("moving the new file in: %w", err)
	}
	os.Remove(old)
	return nil
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
