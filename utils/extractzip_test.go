package utils

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type archiveEntry struct {
	name, body string
	mode       os.FileMode
}

func writeArchive(t *testing.T, entries ...archiveEntry) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "archive.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, entry := range entries {
		h := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		h.SetMode(entry.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return archive
}

func TestExtractZipRejectsEscapingPaths(t *testing.T) {
	for _, name := range []string{"../outside", `..\outside`, "/absolute", `C:\absolute`, `\\server\share`, "sub/../../../outside", "file:stream"} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			outside := filepath.Join(parent, "outside")
			if err := os.WriteFile(outside, []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			archive := writeArchive(t, archiveEntry{name, "overwrite", 0644})
			if err := ExtractZip(archive, filepath.Join(parent, "dest")); err == nil {
				t.Fatal("escaping path was accepted")
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
				t.Fatalf("outside file changed: %q, %v", data, err)
			}
		})
	}
}

func TestExtractZipRejectsEscapingLinks(t *testing.T) {
	for _, target := range []string{"../outside", "/absolute", `C:\absolute`} {
		archive := writeArchive(t, archiveEntry{"link", target, os.ModeSymlink | 0777})
		if err := ExtractZip(archive, t.TempDir()); err == nil {
			t.Fatalf("escaping symlink target accepted: %q", target)
		}
	}
}

func TestExtractZipCannotWriteThroughExistingSymlink(t *testing.T) {
	dst, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dst, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	archive := writeArchive(t, archiveEntry{"link/outside", "bad", 0644})
	if err := ExtractZip(archive, dst); err == nil {
		t.Fatal("write through external symlink succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "outside")); !os.IsNotExist(err) {
		t.Fatalf("file written outside destination: %v", err)
	}
}

func TestExtractZipPreservesBundleFilesAndLinks(t *testing.T) {
	dst := t.TempDir()
	probe := filepath.Join(dst, "probe")
	if err := os.Symlink("target", probe); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	archive := writeArchive(t,
		archiveEntry{`App\bin\run`, "executable", 0755},
		archiveEntry{"App/Versions/A/data", "resource", 0644},
		archiveEntry{"App/Versions/Current", "A", os.ModeSymlink | 0777},
		archiveEntry{"App/Resources", "Versions/Current", os.ModeSymlink | 0777},
	)
	if err := ExtractZip(archive, dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "App", "Resources", "data")); err != nil || string(got) != "resource" {
		t.Fatalf("bundle links: %q, %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dst, "App", "bin", "run"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		t.Fatal("bundle executable lost its executable bit")
	}
}

func TestExtractZipRejectsCorruptEntry(t *testing.T) {
	archive := writeArchive(t, archiveEntry{"script.js", "original", 0644})
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	offset, err := zr.File[0].DataOffset()
	zr.Close()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(archive, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte("X"), offset)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := ExtractZip(archive, t.TempDir()); err == nil {
		t.Fatal("corrupt entry was accepted")
	}
}

func TestExtractZipEntryContainsWrites(t *testing.T) {
	parent := t.TempDir()
	dst := filepath.Join(parent, "extension")
	outside := filepath.Join(parent, "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	archive := writeArchive(t,
		archiveEntry{"scripts/main.js", "script", 0644},
		archiveEntry{"../outside", "overwrite", 0644},
	)
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if err := ExtractZipEntry(zr.File[0], dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "scripts", "main.js")); err != nil || string(got) != "script" {
		t.Fatalf("extension script: %q, %v", got, err)
	}
	if err := ExtractZipEntry(zr.File[1], dst); err == nil {
		t.Fatal("escaping entry was accepted")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "keep" {
		t.Fatalf("outside file changed: %q, %v", got, err)
	}
}
