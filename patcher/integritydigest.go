package patcher

import (
	"crypto/sha256"
	"debug/macho"
	"fmt"
	"maps"
	"os"
	"slices"
)

// Since Electron 41, Electron Framework also holds a SHA-256 of Info.plist's whole
// ElectronAsarIntegrity dictionary, written in when the app is packaged, and Electron
// aborts on launch when the dictionary no longer matches it: "Failed to get integrity
// for validatable asar archive", a SIGTRAP crash (issue #68; electron/electron#48587,
// shell/common/asar/integrity_digest.mm). So when updateAsarIntegrity changes the
// app.asar hash, the digest has to follow.

// asarIntegrityEntry is one archive's entry in the ElectronAsarIntegrity dictionary.
type asarIntegrityEntry struct {
	Algorithm string `json:"algorithm"`
	Hash      string `json:"hash"`
}

// integrityDictDigest is Electron's digest of the dictionary: each entry's path,
// algorithm and hash, in path order. Electron sorts with NSLiteralSearch, which orders
// these ASCII paths the same as sort.Strings.
func integrityDictDigest(dict map[string]asarIntegrityEntry) [sha256.Size]byte {
	h := sha256.New()
	for _, path := range slices.Sorted(maps.Keys(dict)) {
		h.Write([]byte(path))
		h.Write([]byte(dict[path].Algorithm))
		h.Write([]byte(dict[path].Hash))
	}
	return [sha256.Size]byte(h.Sum(nil))
}

// integrityDigestSentinel opens the digest slot: the sentinel, then a used flag and a
// format version (both set at package time), then the digest.
const integrityDigestSentinel = "AGbevlPCksUGKNL8TSn7wGmJEuJsXb2A"

// writeIntegrityDigest stores digest in the slot of each architecture of the Electron
// Framework binary at path, and returns how many it updated. Slots the packager left
// unused are skipped, as Electron doesn't check them, and an Electron older than 41 has
// none.
func writeIntegrityDigest(path string, digest [sha256.Size]byte) (int, error) {
	offsets, err := integrityDigestSlots(path)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %v", path, err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	updated := 0
	for _, off := range offsets {
		var header [len(integrityDigestSentinel) + 2]byte
		if _, err := f.ReadAt(header[:], off); err != nil {
			return 0, fmt.Errorf("reading the integrity digest slot: %v", err)
		}
		if string(header[:len(integrityDigestSentinel)]) != integrityDigestSentinel {
			return 0, fmt.Errorf("no integrity digest slot at %#x in %s", off, path)
		}
		used, version := header[len(header)-2], header[len(header)-1]
		if used == 0 {
			continue
		}
		if version != 1 {
			// Electron rejects every dictionary for a version it doesn't know, so would we.
			return 0, fmt.Errorf("unknown integrity digest version %d in %s", version, path)
		}
		if _, err := f.WriteAt(digest[:], off+int64(len(header))); err != nil {
			return 0, fmt.Errorf("writing the integrity digest: %v", err)
		}
		updated++
	}
	return updated, f.Close()
}

// integrityDigestSlots returns the file offset of the __DATA_CONST,__asar_integrity
// section in each architecture of the (universal or single-architecture) binary at path.
func integrityDigestSlots(path string) ([]int64, error) {
	type arch struct {
		file   *macho.File
		offset int64 // of the architecture within a universal binary
	}
	var archs []arch
	fat, err := macho.OpenFat(path)
	switch err {
	case nil:
		defer fat.Close()
		for _, a := range fat.Arches {
			archs = append(archs, arch{a.File, int64(a.Offset)})
		}
	case macho.ErrNotFat:
		thin, err := macho.Open(path)
		if err != nil {
			return nil, err
		}
		defer thin.Close()
		archs = append(archs, arch{thin, 0})
	default:
		return nil, err
	}

	var offsets []int64
	for _, a := range archs {
		if s := a.file.Section("__asar_integrity"); s != nil && s.Seg == "__DATA_CONST" {
			offsets = append(offsets, a.offset+int64(s.Offset))
		}
	}
	return offsets, nil
}
