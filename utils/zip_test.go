package utils

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExtractZipFileKeepsExecBit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no exec bit on Windows")
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, mode := range map[string]os.FileMode{"disclaimer": 0755, "Info.plist": 0644} {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(mode)
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte(name))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range r.File {
		path := filepath.Join(dir, f.Name)
		if err := ExtractZipFile(f, path); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		wantExec := f.Name == "disclaimer"
		if gotExec := info.Mode()&0111 != 0; gotExec != wantExec {
			t.Errorf("%s: mode %v, want executable=%v", f.Name, info.Mode(), wantExec)
		}
	}
}
