//go:build linux

package patcher

import (
	"strings"
	"testing"
)

func samplePackagesIndex() string {
	return `Package: lib6
Version: 1.2.3
Architecture: amd64
Filename: pool/main/lib6/lib6_1.2.3_amd64.deb
SHA256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

Package: claude-desktop
Version: 1.40602.0
Architecture: amd64
Filename: pool/main/c/claude-desktop/claude-desktop_1.40602.0_amd64.deb
SHA256: bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

Package: claude-desktop
Version: 1.40609.0
Architecture: amd64
Filename: pool/main/c/claude-desktop/claude-desktop_1.40609.0_amd64.deb
SHA256: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
`
}

// TestParseAptNewestClaudeDesk verifies we select the newest claude-desktop
// entry (ignoring other packages and older versions) and carry its filename,
// version and SHA-256 through.
func TestParseAptNewestClaudeDesk(t *testing.T) {
	entry, err := parseAptNewestClaudeDesk(strings.NewReader(samplePackagesIndex()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.version != "1.40609.0" {
		t.Fatalf("expected newest version 1.40609.0, got %s", entry.version)
	}
	if entry.filename != "pool/main/c/claude-desktop/claude-desktop_1.40609.0_amd64.deb" {
		t.Fatalf("unexpected filename: %s", entry.filename)
	}
	if entry.sha256 != "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" {
		t.Fatalf("unexpected sha256: %s", entry.sha256)
	}
}

func TestParseAptNewestNoEntry(t *testing.T) {
	_, err := parseAptNewestClaudeDesk(strings.NewReader("Package: something-else\nVersion: 1.0\n"))
	if err == nil {
		t.Fatalf("expected an error when no claude-desktop entry exists")
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.40609.0", "1.40602.0", 1},
		{"1.40602.0", "1.40609.0", -1},
		{"1.40609.0", "1.40609.0", 0},
		{"1.4.0", "1.10.0", -1},
		{"1.10.0", "1.4.0", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
