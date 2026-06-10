package asar

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRoundTrip packs a synthetic tree, extracts it, and verifies the extracted
// tree matches the source byte-for-byte, exercising nested dirs, empty files,
// and a multi-block (>4 MiB) file.
func TestRoundTrip(t *testing.T) {
	src := t.TempDir()

	bigData := make([]byte, blockSize+12345) // spans two integrity blocks
	if _, err := rand.Read(bigData); err != nil {
		t.Fatal(err)
	}

	files := map[string][]byte{
		"package.json":                      []byte(`{"name":"test","main":"index.js"}`),
		"index.js":                          []byte("console.log('hi')\n"),
		"empty.txt":                         {},
		filepath.Join("a", "b.js"):          []byte("nested"),
		filepath.Join("a", "c.js"):          []byte("nested2"),
		filepath.Join("a", "d", "deep.bin"): bigData,
	}
	for rel, data := range files {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	archive := filepath.Join(t.TempDir(), "test.asar")
	if err := Pack(src, archive); err != nil {
		t.Fatalf("Pack: %v", err)
	}

	dst := t.TempDir()
	if err := Extract(archive, dst); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Errorf("reading %s: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: content mismatch (got %d bytes, want %d)", rel, len(got), len(want))
		}
	}
}

// TestUnpacked verifies that entries marked "unpacked" are read from the
// "<archive>.unpacked" sibling directory rather than the archive body. This
// mirrors how Claude ships its native modules.
func TestUnpacked(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "test.asar")

	// Hand-craft an archive: one inlined file ("normal") and one unpacked
	// entry ("native.node") whose bytes live in the sibling directory only.
	inlined := []byte("inlined-bytes")
	size := int64(len(inlined))
	root := &entry{Files: map[string]*entry{
		"normal.js": {Size: &size, Offset: "0"},
		"native.node": {
			Size:     ptr(int64(0)),
			Unpacked: true,
		},
	}}
	jsonBuf, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	padded := align4(int64(len(jsonBuf)))
	var header [16]byte
	le := func(off int, v uint32) { putUint32(header[:], off, v) }
	le(0, 4)
	le(4, uint32(8+padded))
	le(8, uint32(4+padded))
	le(12, uint32(len(jsonBuf)))

	var buf []byte
	buf = append(buf, header[:]...)
	buf = append(buf, jsonBuf...)
	buf = append(buf, make([]byte, padded-int64(len(jsonBuf)))...)
	buf = append(buf, inlined...)
	if err := os.WriteFile(archive, buf, 0644); err != nil {
		t.Fatal(err)
	}

	// Provide the unpacked file in the sibling directory.
	unpackedDir := archive + ".unpacked"
	os.MkdirAll(unpackedDir, 0755)
	nativeContent := []byte("native-module-bytes")
	os.WriteFile(filepath.Join(unpackedDir, "native.node"), nativeContent, 0644)

	dst := filepath.Join(dir, "out")
	if err := Extract(archive, dst); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(filepath.Join(dst, "normal.js")); string(got) != string(inlined) {
		t.Errorf("inlined file: got %q want %q", got, inlined)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "native.node")); string(got) != string(nativeContent) {
		t.Errorf("unpacked file: got %q want %q", got, nativeContent)
	}
}

func ptr(v int64) *int64 { return &v }

func putUint32(b []byte, off int, v uint32) {
	b[off] = byte(v)
	b[off+1] = byte(v >> 8)
	b[off+2] = byte(v >> 16)
	b[off+3] = byte(v >> 24)
}

// TestPathTraversalRejected ensures malicious index names are refused.
func TestPathTraversalRejected(t *testing.T) {
	if _, err := safeJoin("/dest", "/dest/sub", ".."); err == nil {
		t.Error("expected '..' to be rejected")
	}
	if _, err := safeJoin("/dest", "/dest/sub", "a/b"); err == nil {
		t.Error("expected name with slash to be rejected")
	}
	if _, err := safeJoin("/dest", "/dest/sub", "ok.js"); err != nil {
		t.Errorf("expected valid name to be accepted, got %v", err)
	}
}
