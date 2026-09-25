package patcher

import (
	"archive/tar"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

// Trimmed from the real binary-amd64 Packages index, deliberately out of order and
// with an unrelated package mixed in.
const samplePackages = `Package: claude-desktop
Version: 1.17282.0
Architecture: amd64
Filename: pool/main/c/claude-desktop/claude-desktop_1.17282.0_amd64.deb
SHA256: 1111111111111111111111111111111111111111111111111111111111111111
Description: Desktop application for Claude.ai
 Desktop application for Claude.ai

Package: claude-desktop
Version: 2.7032.0
Architecture: amd64
Filename: pool/main/c/claude-desktop/claude-desktop_2.7032.0_amd64.deb
SHA256: 798E373AF6FA46EDC59B1CB3229A6104FC38CCF66CB9BFEEEA698437FA03F5BD

Package: something-else
Version: 99.0.0
Filename: pool/main/s/something-else/something-else_99.0.0_amd64.deb
SHA256: 2222222222222222222222222222222222222222222222222222222222222222

Package: claude-desktop
Version: 1.40609.0
Architecture: amd64
Filename: pool/main/c/claude-desktop/claude-desktop_1.40609.0_amd64.deb
SHA256: 3333333333333333333333333333333333333333333333333333333333333333
`

func TestParseAptPackagesPicksNewest(t *testing.T) {
	pkg, err := parseAptPackages(strings.NewReader(samplePackages), "claude-desktop")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Version != "2.7032.0" {
		t.Fatalf("version = %s, want 2.7032.0", pkg.Version)
	}
	if pkg.Filename != "pool/main/c/claude-desktop/claude-desktop_2.7032.0_amd64.deb" {
		t.Fatalf("filename = %s", pkg.Filename)
	}
	if pkg.SHA256 != "798e373af6fa46edc59b1cb3229a6104fc38ccf66cb9bfeeea698437fa03f5bd" {
		t.Fatalf("sha256 = %s (should be lowercased)", pkg.SHA256)
	}
}

func TestParseAptPackagesMissing(t *testing.T) {
	if _, err := parseAptPackages(strings.NewReader(samplePackages), "nope"); err == nil {
		t.Fatal("expected an error for a package that isn't in the index")
	}
}

func TestCompareDottedVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.7032.0", "1.40609.0", 1},
		{"1.9.0", "1.10.0", -1},
		{"1.2", "1.2.0", 0},
		{"1.2.1", "1.2", 1},
	}
	for _, c := range cases {
		if got := compareDottedVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

type tarEntry struct {
	hdr  tar.Header
	body string
}

// buildDeb writes a minimal .deb: an ar archive with debian-binary, control.tar.xz and
// an xz-compressed data tar holding entries.
func buildDeb(t *testing.T, entries []tarEntry) string {
	t.Helper()

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, e := range entries {
		h := e.hdr
		h.Size = int64(len(e.body))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()

	var xzBuf bytes.Buffer
	xw, err := xz.NewWriter(&xzBuf)
	if err != nil {
		t.Fatal(err)
	}
	xw.Write(tarBuf.Bytes())
	xw.Close()

	var deb bytes.Buffer
	deb.WriteString("!<arch>\n")
	addMember := func(name string, data []byte) {
		fmt.Fprintf(&deb, "%-16s%-12s%-6s%-6s%-8s%-10d`\n", name, "0", "0", "0", "100644", len(data))
		deb.Write(data)
		if len(data)%2 == 1 {
			deb.WriteByte('\n')
		}
	}
	addMember("debian-binary", []byte("2.0\n"))
	addMember("control.tar.xz", []byte("odd")) // odd length exercises the padding
	addMember("data.tar.xz", xzBuf.Bytes())

	path := filepath.Join(t.TempDir(), "test.deb")
	if err := os.WriteFile(path, deb.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractDebSubtree(t *testing.T) {
	entries := []tarEntry{
		{hdr: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0755}},
		{hdr: tar.Header{Name: "./usr/lib/claude-desktop/", Typeflag: tar.TypeDir, Mode: 0755}},
		{hdr: tar.Header{Name: "./usr/lib/claude-desktop/claude-desktop", Typeflag: tar.TypeReg, Mode: 0755}, body: "binary"},
		{hdr: tar.Header{Name: "./usr/lib/claude-desktop/chrome-sandbox", Typeflag: tar.TypeReg, Mode: 04755}, body: "sandbox"},
		{hdr: tar.Header{Name: "./usr/lib/claude-desktop/resources/", Typeflag: tar.TypeDir, Mode: 0755}},
		{hdr: tar.Header{Name: "./usr/lib/claude-desktop/resources/app.asar", Typeflag: tar.TypeReg, Mode: 0644}, body: "asar"},
		{hdr: tar.Header{Name: "./usr/lib/claude-desktop/resources/app-copy.asar", Typeflag: tar.TypeLink, Linkname: "./usr/lib/claude-desktop/resources/app.asar"}},
		{hdr: tar.Header{Name: "./usr/share/doc/claude-desktop/copyright", Typeflag: tar.TypeReg, Mode: 0644}, body: "outside the prefix"},
	}
	if runtime.GOOS != "windows" { // creating symlinks needs privileges on Windows
		entries = append(entries, tarEntry{hdr: tar.Header{Name: "./usr/lib/claude-desktop/libfoo.so", Typeflag: tar.TypeSymlink, Linkname: "resources/app.asar"}})
	}

	dst := t.TempDir()
	if err := extractDebSubtree(buildDeb(t, entries), "usr/lib/claude-desktop", dst); err != nil {
		t.Fatal(err)
	}

	for rel, want := range map[string]string{
		"claude-desktop":          "binary",
		"chrome-sandbox":          "sandbox",
		"resources/app.asar":      "asar",
		"resources/app-copy.asar": "asar",
	} {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", rel, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "usr")); !os.IsNotExist(err) {
		t.Error("entries outside the prefix were extracted")
	}

	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dst, "claude-desktop"))
		if info.Mode().Perm() != 0755 {
			t.Errorf("claude-desktop mode = %v, want 0755", info.Mode().Perm())
		}
		info, _ = os.Stat(filepath.Join(dst, "chrome-sandbox"))
		if info.Mode()&os.ModeSetuid != 0 {
			t.Error("setuid bit should not be carried over")
		}
		if link, err := os.Readlink(filepath.Join(dst, "libfoo.so")); err != nil || link != "resources/app.asar" {
			t.Errorf("symlink = %q, %v", link, err)
		}
	}
}

func TestExtractDebSubtreeRejectsEscapes(t *testing.T) {
	cases := map[string]tarEntry{
		"symlink outside":  {hdr: tar.Header{Name: "./usr/lib/claude-desktop/evil", Typeflag: tar.TypeSymlink, Linkname: "../../../../etc/passwd"}},
		"absolute symlink": {hdr: tar.Header{Name: "./usr/lib/claude-desktop/evil", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		"hardlink outside": {hdr: tar.Header{Name: "./usr/lib/claude-desktop/evil", Typeflag: tar.TypeLink, Linkname: "./etc/passwd"}},
	}
	for name, evil := range cases {
		t.Run(name, func(t *testing.T) {
			deb := buildDeb(t, []tarEntry{
				{hdr: tar.Header{Name: "./usr/lib/claude-desktop/claude-desktop", Typeflag: tar.TypeReg, Mode: 0755}, body: "binary"},
				evil,
			})
			if err := extractDebSubtree(deb, "usr/lib/claude-desktop", t.TempDir()); err == nil {
				t.Fatal("expected the entry to be rejected")
			}
		})
	}

	// "../" in the entry name itself is normalized away rather than followed.
	deb := buildDeb(t, []tarEntry{
		{hdr: tar.Header{Name: "./usr/lib/claude-desktop/../../../escaped", Typeflag: tar.TypeReg, Mode: 0644}, body: "x"},
		{hdr: tar.Header{Name: "./usr/lib/claude-desktop/claude-desktop", Typeflag: tar.TypeReg, Mode: 0755}, body: "binary"},
	})
	dst := filepath.Join(t.TempDir(), "app")
	if err := extractDebSubtree(deb, "usr/lib/claude-desktop", dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dst), "escaped")); !os.IsNotExist(err) {
		t.Fatal("a ../ entry escaped the destination")
	}
}
