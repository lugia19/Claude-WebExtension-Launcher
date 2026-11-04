package patcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Claude 4.4212.0 (Electron 44.9.3): the dictionary its Info.plist ships with, and the
// digest its Electron Framework holds for it, in both architectures.
func TestIntegrityDictDigest(t *testing.T) {
	dict := map[string]asarIntegrityEntry{
		"Resources/ion-dist.asar": {"SHA256", "ba7a5bcd05df7d52931a393053d070f06a3f44fb00adcbcc52e9c25ca248b866"},
		"Resources/app.asar":      {"SHA256", "2d8822229ac8cb900afb49c4bb2bd9893d7a7791e39b1308ac98e85e78ab2b69"},
	}
	const want = "479881ae68b158752618bcb7a92c417b22a968435932384f49f672398c32a7e3"

	digest := integrityDictDigest(dict)
	if got := hex.EncodeToString(digest[:]); got != want {
		t.Fatalf("integrityDictDigest = %s, want %s", got, want)
	}
}

// writeIntegrityDigest finds slots by their sentinel anywhere in the file, writes the
// digest into version-1 slots, disables (used = 0) slots of a version it doesn't know,
// and leaves unused slots and stray copies of the sentinel alone.
func TestWriteIntegrityDigest(t *testing.T) {
	const slotLen = len(integrityDigestSentinel) + 2 + sha256.Size
	type slot struct {
		name          string
		off           int
		used, version byte
		wantDigest    bool // the digest is written
		wantUsed      byte
	}
	slots := []slot{
		{"version 1", 100, 1, 1, true, 1},
		{"across a read chunk", 1<<20 - 10, 1, 1, true, 1},
		{"unknown version", 2<<20 + 5, 1, 2, false, 0},
		{"unused", 2<<20 + 500, 0, 0, false, 0},
	}
	data := make([]byte, 3<<20)
	for _, s := range slots {
		copy(data[s.off:], integrityDigestSentinel)
		data[s.off+len(integrityDigestSentinel)] = s.used
		data[s.off+len(integrityDigestSentinel)+1] = s.version
	}
	// The sentinel as a C string elsewhere: its "used" byte is the terminating NUL.
	stray := 2<<20 + 900
	copy(data[stray:], integrityDigestSentinel+"\x00\x01\x01rest")

	path := filepath.Join(t.TempDir(), "Electron Framework")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("dictionary"))
	written, disabled, err := writeIntegrityDigest(path, digest)
	if err != nil {
		t.Fatal(err)
	}
	if written != 2 || disabled != 1 {
		t.Errorf("written %d, disabled %d; want 2, 1", written, disabled)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(data) {
		t.Fatalf("file size changed: %d -> %d", len(data), len(got))
	}
	for _, s := range slots {
		usedAt := s.off + len(integrityDigestSentinel)
		if got[usedAt] != s.wantUsed {
			t.Errorf("%s: used = %d, want %d", s.name, got[usedAt], s.wantUsed)
		}
		want := make([]byte, sha256.Size)
		if s.wantDigest {
			want = digest[:]
		}
		if !bytes.Equal(got[usedAt+2:s.off+slotLen], want) {
			t.Errorf("%s: digest bytes = %x, want %x", s.name, got[usedAt+2:s.off+slotLen], want)
		}
	}
	if !bytes.Equal(got[stray:stray+slotLen], data[stray:stray+slotLen]) {
		t.Error("the stray sentinel copy was changed")
	}
}
