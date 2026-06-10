// Package asar implements reading and writing of Electron asar archives using
// only the standard library, replacing the Node.js @electron/asar CLI tool.
//
// An asar archive is laid out as:
//
//	[16-byte header] [JSON index] [padding to 4 bytes] [concatenated file bytes]
//
// The 16-byte header is four little-endian uint32 "pickle" words:
//
//	word0 = 4                      (size of the outer pickle payload)
//	word1 = 8 + align4(jsonLen)    (size of the inner pickle)
//	word2 = 4 + align4(jsonLen)    (inner pickle payload size)
//	word3 = jsonLen                (byte length of the JSON index)
//
// File contents begin at offset 8+word1 (== 16+align4(jsonLen)). Each file's
// "offset" in the index is a decimal string relative to that content base.
package asar

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const blockSize = 4 * 1024 * 1024 // 4 MiB, matches @electron/asar

type integrity struct {
	Algorithm string   `json:"algorithm"`
	Hash      string   `json:"hash"`
	BlockSize int      `json:"blockSize"`
	Blocks    []string `json:"blocks"`
}

type entry struct {
	Files      map[string]*entry `json:"files,omitempty"`
	Size       *int64            `json:"size,omitempty"`
	Offset     string            `json:"offset,omitempty"`
	Unpacked   bool              `json:"unpacked,omitempty"`
	Executable bool              `json:"executable,omitempty"`
	Link       string            `json:"link,omitempty"`
	Integrity  *integrity        `json:"integrity,omitempty"`
}

func align4(n int64) int64 {
	if r := n % 4; r != 0 {
		return n + (4 - r)
	}
	return n
}

// Extract unpacks the asar archive at asarPath into destDir. Files marked
// "unpacked" are copied from the sibling "<asarPath>.unpacked" directory.
func Extract(asarPath, destDir string) error {
	f, err := os.Open(asarPath)
	if err != nil {
		return fmt.Errorf("opening asar: %w", err)
	}
	defer f.Close()

	var header [16]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return fmt.Errorf("reading asar header: %w", err)
	}
	word0 := binary.LittleEndian.Uint32(header[0:4])
	word1 := binary.LittleEndian.Uint32(header[4:8])
	jsonLen := binary.LittleEndian.Uint32(header[12:16])
	if word0 != 4 {
		return fmt.Errorf("invalid asar header (word0=%d, expected 4)", word0)
	}

	jsonBuf := make([]byte, jsonLen)
	if _, err := io.ReadFull(f, jsonBuf); err != nil {
		return fmt.Errorf("reading asar index: %w", err)
	}
	var root entry
	if err := json.Unmarshal(jsonBuf, &root); err != nil {
		return fmt.Errorf("parsing asar index: %w", err)
	}

	contentBase := int64(8) + int64(word1)
	unpackedDir := asarPath + ".unpacked"

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	return extractEntry(&root, f, contentBase, destDir, destDir, unpackedDir)
}

func extractEntry(e *entry, archive *os.File, contentBase int64, destDir, curDir, unpackedDir string) error {
	for name, child := range e.Files {
		dst, err := safeJoin(destDir, curDir, name)
		if err != nil {
			return err
		}

		switch {
		case child.Files != nil:
			if err := os.MkdirAll(dst, 0755); err != nil {
				return err
			}
			if err := extractEntry(child, archive, contentBase, destDir, dst, unpackedDir); err != nil {
				return err
			}

		case child.Link != "":
			os.Remove(dst)
			if err := os.Symlink(child.Link, dst); err != nil {
				return fmt.Errorf("creating symlink %s: %w", dst, err)
			}

		case child.Unpacked:
			rel, err := filepath.Rel(destDir, dst)
			if err != nil {
				return err
			}
			if err := copyFile(filepath.Join(unpackedDir, rel), dst, child.Executable); err != nil {
				return fmt.Errorf("copying unpacked file %s: %w", rel, err)
			}

		default:
			if err := writeArchiveFile(child, archive, contentBase, dst); err != nil {
				return fmt.Errorf("extracting %s: %w", dst, err)
			}
		}
	}
	return nil
}

func writeArchiveFile(e *entry, archive *os.File, contentBase int64, dst string) error {
	if e.Size == nil {
		return fmt.Errorf("file entry missing size")
	}
	offset, err := strconv.ParseInt(e.Offset, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing offset %q: %w", e.Offset, err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := archive.Seek(contentBase+offset, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.CopyN(out, archive, *e.Size); err != nil {
		return err
	}
	if e.Executable && runtime.GOOS != "windows" {
		return os.Chmod(dst, 0755)
	}
	return nil
}

func copyFile(src, dst string, executable bool) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if executable && runtime.GOOS != "windows" {
		return os.Chmod(dst, 0755)
	}
	return nil
}

// safeJoin joins name onto curDir and verifies the result stays within destDir,
// rejecting path-traversal entries in the archive index.
func safeJoin(destDir, curDir, name string) (string, error) {
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) ||
		name == ".." || name == "." || name == "" {
		return "", fmt.Errorf("unsafe entry name %q", name)
	}
	dst := filepath.Join(curDir, name)
	rel, err := filepath.Rel(destDir, dst)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entry %q escapes destination", name)
	}
	return dst, nil
}

// packFile is a regular file queued for writing, in index/offset order.
type packFile struct {
	diskPath string
	entry    *entry
}

// Pack builds an asar archive at asarPath from the contents of srcDir. Every
// regular file is inlined into the archive (nothing is left "unpacked").
func Pack(srcDir, asarPath string) error {
	root := &entry{Files: map[string]*entry{}}
	var files []packFile
	var offset int64

	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		node, err := ensureParents(root, rel)
		if err != nil {
			return err
		}

		switch {
		case d.IsDir():
			node.Files = map[string]*entry{}

		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			node.Link = target

		default:
			info, err := d.Info()
			if err != nil {
				return err
			}
			size := info.Size()
			integ, err := computeIntegrity(path)
			if err != nil {
				return err
			}
			node.Size = &size
			node.Offset = strconv.FormatInt(offset, 10)
			node.Integrity = integ
			if runtime.GOOS != "windows" && info.Mode()&0111 != 0 {
				node.Executable = true
			}
			files = append(files, packFile{diskPath: path, entry: node})
			offset += size
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking %s: %w", srcDir, err)
	}

	jsonBuf, err := json.Marshal(root)
	if err != nil {
		return err
	}
	jsonLen := int64(len(jsonBuf))
	padded := align4(jsonLen)

	var header [16]byte
	binary.LittleEndian.PutUint32(header[0:4], 4)
	binary.LittleEndian.PutUint32(header[4:8], uint32(8+padded))
	binary.LittleEndian.PutUint32(header[8:12], uint32(4+padded))
	binary.LittleEndian.PutUint32(header[12:16], uint32(jsonLen))

	tmp, err := os.CreateTemp(filepath.Dir(asarPath), "asar-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	w := tmp
	if _, err := w.Write(header[:]); err != nil {
		tmp.Close()
		return err
	}
	if _, err := w.Write(jsonBuf); err != nil {
		tmp.Close()
		return err
	}
	if pad := padded - jsonLen; pad > 0 {
		if _, err := w.Write(make([]byte, pad)); err != nil {
			tmp.Close()
			return err
		}
	}
	for _, pf := range files {
		in, err := os.Open(pf.diskPath)
		if err != nil {
			tmp.Close()
			return err
		}
		if _, err := io.Copy(w, in); err != nil {
			in.Close()
			tmp.Close()
			return err
		}
		in.Close()
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, asarPath)
}

// ensureParents descends/creates the directory entries for rel's parents and
// returns the (newly created) leaf entry.
func ensureParents(root *entry, rel string) (*entry, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	cur := root
	for i, part := range parts {
		if i == len(parts)-1 {
			child := &entry{}
			cur.Files[part] = child
			return child, nil
		}
		next, ok := cur.Files[part]
		if !ok || next.Files == nil {
			return nil, fmt.Errorf("missing parent directory for %q", rel)
		}
		cur = next
	}
	return nil, fmt.Errorf("empty relative path")
}

func computeIntegrity(path string) (*integrity, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	whole := sha256.New()
	var blocks []string
	buf := make([]byte, blockSize)
	for {
		n, err := io.ReadFull(f, buf)
		if n > 0 {
			whole.Write(buf[:n])
			bh := sha256.Sum256(buf[:n])
			blocks = append(blocks, hex.EncodeToString(bh[:]))
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	// A zero-length file still gets a single (empty-content) block.
	if len(blocks) == 0 {
		bh := sha256.Sum256(nil)
		blocks = append(blocks, hex.EncodeToString(bh[:]))
	}
	return &integrity{
		Algorithm: "SHA256",
		Hash:      hex.EncodeToString(whole.Sum(nil)),
		BlockSize: blockSize,
		Blocks:    blocks,
	}, nil
}
