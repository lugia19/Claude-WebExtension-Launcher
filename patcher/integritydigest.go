package patcher

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
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

// writeIntegrityDigest updates every digest slot in the Electron Framework binary at
// path (one per architecture in a universal binary). Slots the packager left unused are
// skipped: Electron doesn't check them, and an Electron older than 41 has none.
//
// The slots are found by their sentinel, as the Windows DLL finds the header hash, so
// a move to another section can't make us miss one and install a Claude that crashes.
// A slot with a format version we don't know gets its used flag cleared instead, which
// Electron treats as "no digest" (fail open), rather than holding a digest it rejects.
func writeIntegrityDigest(path string, digest [sha256.Size]byte) (written, disabled int, err error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	offsets, err := findAll(f, []byte(integrityDigestSentinel))
	if err != nil {
		return 0, 0, fmt.Errorf("reading %s: %v", path, err)
	}
	for _, off := range offsets {
		usedAt := off + int64(len(integrityDigestSentinel))
		var flags [2]byte // used, version
		if _, err := f.ReadAt(flags[:], usedAt); err != nil {
			return 0, 0, fmt.Errorf("reading the integrity digest slot: %v", err)
		}
		switch {
		case flags[0] == 0:
			continue
		case flags[1] == 1:
			if _, err := f.WriteAt(digest[:], usedAt+2); err != nil {
				return 0, 0, fmt.Errorf("writing the integrity digest: %v", err)
			}
			written++
		default:
			fmt.Printf("Unknown integrity digest version %d at %#x, disabling the check there\n", flags[1], off)
			if _, err := f.WriteAt([]byte{0}, usedAt); err != nil {
				return 0, 0, fmt.Errorf("disabling the integrity digest: %v", err)
			}
			disabled++
		}
	}
	return written, disabled, f.Close()
}

// findAll returns the offset of every occurrence of needle in f, reading it in chunks
// (Electron Framework is a few hundred MB).
func findAll(f *os.File, needle []byte) ([]int64, error) {
	const chunk = 1 << 20
	// Each read overlaps the next chunk by len(needle)-1 bytes, so a match that crosses
	// a chunk boundary is found once, in the chunk where it starts.
	buf := make([]byte, chunk+len(needle)-1)
	var offsets []int64
	for pos := int64(0); ; pos += chunk {
		n, err := f.ReadAt(buf, pos)
		for i := 0; ; {
			j := bytes.Index(buf[i:n], needle)
			if j < 0 {
				break
			}
			offsets = append(offsets, pos+int64(i+j))
			i += j + 1
		}
		if err == io.EOF {
			return offsets, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
