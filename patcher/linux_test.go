package patcher

import (
	"claude-webext-patcher/asar"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAsar writes a minimal valid asar file: the 16-byte header
// (word0=4, jsonLen at bytes 12:16) followed by the JSON index. HeaderHash
// only reads these bytes, so this is enough to exercise it.
func writeAsar(t *testing.T, path, index string) {
	t.Helper()
	var header [16]byte
	binary.LittleEndian.PutUint32(header[0:4], 4)
	binary.LittleEndian.PutUint32(header[12:16], uint32(len(index)))
	data := append(header[:], []byte(index)...)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// fixtureLinux returns a fake system-install tree under tmp, mimicking the
// official package layout.
func fixtureLinux(t *testing.T) *linuxInstall {
	t.Helper()
	base := t.TempDir()
	bin := filepath.Join(base, "claude-desktop")
	resources := filepath.Join(base, "resources")
	asarPath := filepath.Join(resources, "app.asar")
	if err := os.MkdirAll(resources, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	writeAsar(t, asarPath, `{"files":{}}`)
	if err := os.WriteFile(filepath.Join(base, "version"), []byte("9.9.9"), 0644); err != nil {
		t.Fatal(err)
	}
	return &linuxInstall{
		baseDir:      base,
		resourcesDir: resources,
		electronBin:  bin,
		asarPath:     asarPath,
		version:      "9.9.9",
	}
}

func TestDetectLinuxInstall(t *testing.T) {
	install := fixtureLinux(t)
	dirs := linuxInstallDirs
	linuxInstallDirs = append([]string{install.baseDir}, dirs...)
	defer func() { linuxInstallDirs = dirs }()

	got, err := detectLinuxInstall()
	if err != nil {
		t.Fatalf("detectLinuxInstall: %v", err)
	}
	if got.baseDir != install.baseDir {
		t.Fatalf("baseDir = %q, want %q", got.baseDir, install.baseDir)
	}
	if got.version != "9.9.9" {
		t.Fatalf("version = %q, want 9.9.9", got.version)
	}
}

func TestDetectLinuxInstallMissing(t *testing.T) {
	dirs := linuxInstallDirs
	linuxInstallDirs = []string{filepath.Join(t.TempDir(), "nope")}
	defer func() { linuxInstallDirs = dirs }()

	if _, err := detectLinuxInstall(); err == nil {
		t.Fatal("expected error when no install present, got nil")
	}
}

func TestSystemAsarHashMatches(t *testing.T) {
	install := fixtureLinux(t)
	dataDir := t.TempDir()
	hashPath := filepath.Join(dataDir, "patched-asar-hash.txt")

	// No stamp file yet -> not patched.
	if patched, _ := systemAsarHashMatches(install, dataDir); patched {
		t.Fatal("expected not patched when no stamp exists")
	}

	// Stamp matching the current hash -> patched.
	hash, err := asar.HeaderHash(install.asarPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hashPath, []byte(hash), 0644); err != nil {
		t.Fatal(err)
	}
	if patched, _ := systemAsarHashMatches(install, dataDir); !patched {
		t.Fatal("expected patched when stamp matches hash")
	}

	// Simulate a system update overwriting the asar -> hash differs -> repatch.
	writeAsar(t, install.asarPath, `{"files":{"updated":{}}}`)
	if patched, _ := systemAsarHashMatches(install, dataDir); patched {
		t.Fatal("expected not patched after asar changed")
	}
}

func TestPatchProtocolArrayDoubleQuoted(t *testing.T) {
	in := []byte(`xyz = ["devtools:","file:","app:"], rest`)
	out, ok := patchProtocolArray(in)
	if !ok {
		t.Fatal("expected patch to match double-quoted form")
	}
	s := string(out)
	if !strings.Contains(s, `,"chrome-extension:"`) {
		t.Fatalf("chrome-extension not inserted: %s", s)
	}
}

func TestPatchProtocolArrayBacktickQuoted(t *testing.T) {
	in := []byte("new Set([`devtools:`,`file:`,`app:`])")
	out, ok := patchProtocolArray(in)
	if !ok {
		t.Fatal("expected patch to match backtick-quoted form")
	}
	s := string(out)
	if !strings.Contains(s, ",`chrome-extension:`") {
		t.Fatalf("chrome-extension not inserted in backtick form: %s", s)
	}
}

func TestPatchProtocolArrayAlreadyPresent(t *testing.T) {
	in := []byte("new Set([`devtools:`,`file:`,`chrome-extension:`])")
	_, ok := patchProtocolArray(in)
	if ok {
		t.Fatal("expected no patch when chrome-extension already present")
	}
}

func TestPatchProtocolArrayNoMatch(t *testing.T) {
	in := []byte(`something entirely different`)
	out, ok := patchProtocolArray(in)
	if ok || len(out) != len(in) {
		t.Fatal("expected no match and unchanged output")
	}
}
