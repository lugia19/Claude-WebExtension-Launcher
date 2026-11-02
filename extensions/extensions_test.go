package extensions

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func extensionZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFailedUpdateKeepsInstalledExtension(t *testing.T) {
	validManifest := `{"manifest_version":3,"name":"Test extension","version":"2.0"}`
	corrupt := extensionZip(t, map[string]string{"manifest.json": validManifest, "script.js": "new script"})
	zr, err := zip.NewReader(bytes.NewReader(corrupt), int64(len(corrupt)))
	if err != nil {
		t.Fatal(err)
	}
	offset, err := zr.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	corrupt[offset] ^= 1 // still a readable ZIP, but extraction must fail its CRC
	cases := []struct {
		name   string
		status int
		body   []byte
	}{
		{"HTTP error", 404, []byte("not found")},
		{"invalid ZIP", 200, []byte("not a ZIP")},
		{"truncated ZIP", 200, extensionZip(t, map[string]string{"manifest.json": validManifest})[:30]},
		{"corrupt entry", 200, corrupt},
		{"missing manifest", 200, extensionZip(t, map[string]string{"script.js": "new"})},
		{"invalid manifest", 200, extensionZip(t, map[string]string{"manifest.json": "{"})},
		{"incomplete manifest", 200, extensionZip(t, map[string]string{"manifest.json": `{"version":"2.0"}`})},
		{"invalid version", 200, extensionZip(t, map[string]string{"manifest.json": `{"manifest_version":3,"name":"Broken","version":"broken"}`})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parent := t.TempDir()
			dst := filepath.Join(parent, "extension")
			if err := os.Mkdir(dst, 0755); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(dst, "manifest.json")
			old := []byte(`{"manifest_version":3,"name":"Old","version":"1.0"}`)
			if err := os.WriteFile(manifest, old, 0644); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				w.Write(c.body)
			}))
			defer srv.Close()
			if err := installExtension(srv.URL, dst); err == nil {
				t.Fatal("invalid update succeeded")
			}
			got, err := os.ReadFile(manifest)
			if err != nil || !bytes.Equal(got, old) {
				t.Fatalf("installed extension changed: %q, %v", got, err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil || len(entries) != 1 || entries[0].Name() != "extension" {
				t.Fatalf("failed update left files outside the installed extension: %v, %v", entries, err)
			}
		})
	}
}

func TestExtensionVersionFormat(t *testing.T) {
	for _, version := range []string{"1", "1.2", "1.2.3.4", "0.1.0.0", "65535.65535.65535.65535"} {
		if !validExtensionVersion(version) {
			t.Errorf("valid version rejected: %q", version)
		}
	}
	for _, version := range []string{"", "broken", "0", "0.0.0.0", "01.2", "+1", "-1", "1..2", "1.", ".1", "1.2.3.4.5", "65536", "1.2beta", " 1", "1 "} {
		if validExtensionVersion(version) {
			t.Errorf("invalid version accepted: %q", version)
		}
	}
}

func TestSuccessfulUpdateReplacesWholeExtension(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "extension")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "obsolete.js"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	body := extensionZip(t, map[string]string{
		"manifest.json":   `{"manifest_version":3,"name":"Test","version":"2.0"}`,
		"scripts/main.js": "new script",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	if err := installExtension(srv.URL, dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "scripts", "main.js")); err != nil || string(got) != "new script" {
		t.Fatalf("new script: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "obsolete.js")); !os.IsNotExist(err) {
		t.Fatalf("obsolete file survived replacement: %v", err)
	}
}
