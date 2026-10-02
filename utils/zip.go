package utils

import (
	"archive/zip"
	"io"
	"os"
)

// ExtractZipFile writes one zip entry to path. Errors matter here: the zip reader
// reports CRC mismatches from Read, so a truncated or corrupt download fails instead
// of silently leaving a partial file behind.
//
// An entry stored as executable stays executable (issue #56: Claude spawns Claude Code
// through Contents/Helpers/disclaimer, which failed with EACCES when extracted 0644).
func ExtractZipFile(f *zip.File, path string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if f.Mode()&0111 != 0 {
		return os.Chmod(path, 0755)
	}
	return nil
}
