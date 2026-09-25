package utils

import (
	"archive/zip"
	"io"
	"os"
)

// ExtractZipFile writes one zip entry to path. Errors matter here: the zip reader
// reports CRC mismatches from Read, so a truncated or corrupt download fails instead
// of silently leaving a partial file behind.
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
	return dst.Close()
}
