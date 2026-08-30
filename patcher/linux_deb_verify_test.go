//go:build linux

package patcher

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDebFixture writes a temp file containing the given bytes and returns its
// path and SHA-256 digest (lowercase hex).
func writeDebFixture(t *testing.T, content []byte) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "claude-test.deb")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	sum := sha256.Sum256(content)
	return path, hex.EncodeToString(sum[:])
}

func TestVerifyDEBSHA256Valid(t *testing.T) {
	path, want := writeDebFixture(t, []byte("claude-desktop_1.40609.0_amd64.deb"))
	if err := verifyDEBSHA256(path, want); err != nil {
		t.Fatalf("expected valid hash to pass, got: %v", err)
	}
}

// TestVerifyDEBSHA256Invalid confirms a mismatched digest aborts installation
// (the caller removes the download).
func TestVerifyDEBSHA256Invalid(t *testing.T) {
	path, _ := writeDebFixture(t, []byte("claude-desktop_1.40609.0_amd64.deb"))
	bad := strings.Repeat("0", 64)
	err := verifyDEBSHA256(path, bad)
	if err == nil {
		t.Fatal("expected a mismatch error")
	}
	if !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("expected mismatch error, got: %v", err)
	}
	if !strings.Contains(err.Error(), bad) {
		t.Fatalf("error should report the expected hash, got: %v", err)
	}
}

// TestVerifyDEBSHA256Missing enforces the refusal when the index carried no
// digest: an empty expected value must never install.
func TestVerifyDEBSHA256Missing(t *testing.T) {
	path, _ := writeDebFixture(t, []byte("claude-desktop_1.40609.0_amd64.deb"))
	if err := verifyDEBSHA256(path, ""); err == nil {
		t.Fatal("expected refusal when expected hash is missing")
	}
}

// TestVerifyDEBSHA256CaseInsensitive mirrors how the index and a hex digest
// could disagree only in casing; both must still match.
func TestVerifyDEBSHA256CaseInsensitive(t *testing.T) {
	path, want := writeDebFixture(t, []byte("claude-desktop_1.40609.0_amd64.deb"))
	if err := verifyDEBSHA256(path, strings.ToUpper(want)); err != nil {
		t.Fatalf("expected case-insensitive match to pass, got: %v", err)
	}
}
