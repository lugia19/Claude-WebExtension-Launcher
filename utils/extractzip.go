package utils

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// zipPath accepts both ZIP and PowerShell separators, but never absolute paths,
// Windows drive/stream names, reserved device names, or paths escaping the root.
func zipPath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	local := filepath.FromSlash(name)
	if !filepath.IsLocal(local) || strings.Contains(name, ":") {
		return "", fmt.Errorf("refusing archive path outside the destination: %q", name)
	}
	return local, nil
}

// ExtractZip extracts into dst using os.Root so writes cannot escape through
// existing symlinks (or links created by earlier entries). Bundle-relative symlinks
// and executable bits are preserved for macOS and Linux updates.
func ExtractZip(archive, dst string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()

	for _, f := range zr.File {
		if err := extractZipEntry(root, f); err != nil {
			return err
		}
	}
	return nil
}

// ExtractZipEntry confines one entry to dst. Callers that already own the ZIP
// reader can keep their download and installation lifecycle separate.
func ExtractZipEntry(f *zip.File, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	return extractZipEntry(root, f)
}

func extractZipEntry(root *os.Root, f *zip.File) error {
	name, err := zipPath(f.Name)
	if err != nil {
		return err
	}
	if f.FileInfo().IsDir() || strings.HasSuffix(name, string(filepath.Separator)) {
		if err := root.MkdirAll(name, 0755); err != nil {
			return fmt.Errorf("creating archive directory %q: %w", f.Name, err)
		}
		return nil
	}
	if err := root.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return fmt.Errorf("creating parent of %q: %w", f.Name, err)
	}
	if err := writeZipEntry(root, f, name); err != nil {
		return fmt.Errorf("extracting %q: %w", f.Name, err)
	}
	return nil
}

func writeZipEntry(root *os.Root, f *zip.File, name string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	if f.Mode()&os.ModeSymlink != 0 {
		target, err := io.ReadAll(io.LimitReader(src, 4097))
		if err != nil {
			return err
		}
		link := strings.ReplaceAll(string(target), "\\", "/")
		if len(target) == 0 || len(target) > 4096 || strings.HasPrefix(link, "/") || strings.Contains(link, ":") {
			return fmt.Errorf("refusing symlink target %q", link)
		}
		if _, err := zipPath(filepath.Join(filepath.Dir(name), filepath.FromSlash(link))); err != nil {
			return err
		}
		return root.Symlink(filepath.FromSlash(link), name)
	}
	if !f.Mode().IsRegular() {
		return fmt.Errorf("unsupported archive file type: %s", f.Mode())
	}
	dst, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if err == nil && f.Mode()&0111 != 0 {
		err = dst.Chmod(0755)
	}
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	return err
}
