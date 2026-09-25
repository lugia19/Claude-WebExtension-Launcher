package patcher

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ulikunitz/xz"
)

// Linux ships Claude as a .deb in Anthropic's APT repository. These helpers read the
// repository index and unpack the .deb without dpkg, ar or tar, so the launcher stays
// free of runtime tool dependencies. They carry no build tag so they can be unit-tested
// on any platform.

// aptPackage is one stanza of an APT Packages index.
type aptPackage struct {
	Version  string
	Filename string
	SHA256   string
}

// parseAptPackages returns the newest stanza for pkg in an APT Packages index. The
// index lists every published version and makes no ordering promise, so versions are
// compared rather than taking the first or last stanza.
func parseAptPackages(r io.Reader, pkg string) (aptPackage, error) {
	var newest, cur aptPackage
	curName := ""
	found := false

	flush := func() {
		if curName == pkg && cur.Version != "" && cur.Filename != "" && cur.SHA256 != "" {
			if !found || compareDottedVersions(cur.Version, newest.Version) > 0 {
				newest = cur
				found = true
			}
		}
		cur = aptPackage{}
		curName = ""
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue // continuation of a multi-line field (e.g. Description)
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Package":
			curName = value
		case "Version":
			cur.Version = value
		case "Filename":
			cur.Filename = value
		case "SHA256":
			cur.SHA256 = strings.ToLower(value)
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return aptPackage{}, fmt.Errorf("reading Packages index: %v", err)
	}
	if !found {
		return aptPackage{}, fmt.Errorf("no %s entry found in Packages index", pkg)
	}
	return newest, nil
}

// compareDottedVersions compares dotted numeric versions ("2.7032.0"), returning -1, 0
// or 1. Missing or non-numeric components count as 0.
func compareDottedVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var na, nb int
		if i < len(pa) {
			na, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			nb, _ = strconv.Atoi(pb[i])
		}
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
	}
	return 0
}

// extractDebSubtree unpacks the part of a .deb's data payload that lives under prefix
// (e.g. "usr/lib/claude-desktop") into dst, with prefix stripped. Directories, regular
// files (with their permission bits), symlinks and hardlinks are supported; anything
// that would land outside dst is rejected.
func extractDebSubtree(debPath, prefix, dst string) error {
	f, err := os.Open(debPath)
	if err != nil {
		return err
	}
	defer f.Close()

	name, member, err := findDataMember(f)
	if err != nil {
		return err
	}

	var payload io.Reader
	switch {
	case strings.HasSuffix(name, ".tar.xz"):
		xr, err := xz.NewReader(bufio.NewReaderSize(member, 1<<20))
		if err != nil {
			return fmt.Errorf("opening %s: %v", name, err)
		}
		payload = xr
	case strings.HasSuffix(name, ".tar.gz"):
		gr, err := gzip.NewReader(member)
		if err != nil {
			return fmt.Errorf("opening %s: %v", name, err)
		}
		defer gr.Close()
		payload = gr
	case strings.HasSuffix(name, ".tar"):
		payload = member
	default:
		return fmt.Errorf("unsupported .deb payload compression: %s", name)
	}

	return extractTarSubtree(tar.NewReader(payload), strings.Trim(prefix, "/"), dst)
}

// findDataMember walks the ar archive that wraps a .deb and returns the data.tar.*
// member. ar layout: "!<arch>\n", then per member a 60-byte header (16-byte name,
// 10-byte decimal size at offset 48) followed by the data, padded to an even length.
func findDataMember(f *os.File) (string, io.Reader, error) {
	magic := make([]byte, 8)
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != "!<arch>\n" {
		return "", nil, fmt.Errorf("not a .deb (missing ar header)")
	}

	offset := int64(8)
	header := make([]byte, 60)
	for {
		if _, err := f.ReadAt(header, offset); err != nil {
			if err == io.EOF {
				return "", nil, fmt.Errorf(".deb has no data.tar member")
			}
			return "", nil, fmt.Errorf("reading .deb member header: %v", err)
		}
		if !bytes.Equal(header[58:60], []byte("`\n")) {
			return "", nil, fmt.Errorf("corrupt .deb member header at offset %d", offset)
		}
		name := strings.TrimSuffix(strings.TrimSpace(string(header[0:16])), "/")
		size, err := strconv.ParseInt(strings.TrimSpace(string(header[48:58])), 10, 64)
		if err != nil || size < 0 {
			return "", nil, fmt.Errorf("corrupt .deb member size for %q", name)
		}
		dataStart := offset + 60
		if strings.HasPrefix(name, "data.tar") {
			return name, io.NewSectionReader(f, dataStart, size), nil
		}
		offset = dataStart + size + size%2
	}
}

func extractTarSubtree(tr *tar.Reader, prefix, dst string) error {
	// relPath maps an archive path to its path relative to dst, or ok=false when it
	// is outside prefix. Archive paths are usually "./usr/lib/...".
	relPath := func(name string) (string, bool) {
		name = strings.TrimPrefix(path.Clean("/"+name), "/")
		if name == prefix {
			return "", true
		}
		if !strings.HasPrefix(name, prefix+"/") {
			return "", false
		}
		return strings.TrimPrefix(name, prefix+"/"), true
	}

	extracted := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading .deb payload: %v", err)
		}

		rel, ok := relPath(hdr.Name)
		if !ok || rel == "" {
			continue
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		mode := os.FileMode(hdr.Mode) & os.ModePerm

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			os.Chmod(target, mode|0700)

		case tar.TypeReg:
			if err := writeTarFile(tr, target, mode); err != nil {
				return fmt.Errorf("extracting %s: %v", rel, err)
			}
			extracted++

		case tar.TypeSymlink:
			// Relative links must stay inside the tree; absolute ones are refused.
			resolved := path.Join(path.Dir(rel), hdr.Linkname)
			if path.IsAbs(hdr.Linkname) || resolved == ".." || strings.HasPrefix(resolved, "../") {
				return fmt.Errorf("refusing symlink %s -> %s (points outside the app)", rel, hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("creating symlink %s: %v", rel, err)
			}

		case tar.TypeLink:
			linkRel, ok := relPath(hdr.Linkname)
			if !ok || linkRel == "" {
				return fmt.Errorf("refusing hardlink %s -> %s (points outside the app)", rel, hdr.Linkname)
			}
			source := filepath.Join(dst, filepath.FromSlash(linkRel))
			os.Remove(target)
			if err := os.Link(source, target); err != nil {
				// Fall back to a copy (e.g. filesystems without hardlinks).
				if err := copyLocalFile(source, target); err != nil {
					return fmt.Errorf("creating hardlink %s: %v", rel, err)
				}
			}
			extracted++
		}
	}

	if extracted == 0 {
		return fmt.Errorf(".deb payload contains nothing under %s", prefix)
	}
	return nil
}

func writeTarFile(r io.Reader, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	os.Remove(target)
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// OpenFile's mode is filtered by the umask; set the archive's bits exactly.
	return os.Chmod(target, mode|0600)
}

func copyLocalFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	return writeTarFile(in, target, info.Mode()&os.ModePerm)
}
