//go:build !windows

package patcher

import (
	"archive/zip"
	"bufio"
	"claude-webext-patcher/asar"
	"claude-webext-patcher/utils"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func initPaths() {
	installBaseDir = utils.ResolvePath(".")
	setAppPaths(utils.ResolvePath(appFolderName))
}

// setAppPaths points the app-folder globals at appFolder. buildAndSwap uses this to
// build a patched install in a staging folder before swapping it into place.
//
// macOS ships a .app bundle (Claude.app/Contents/Resources, Contents/MacOS/Claude),
// while the Linux .deb is a flat Electron layout (resources/app.asar beside the
// claude-desktop executable).
func setAppPaths(appFolder string) {
	AppFolder = appFolder
	if runtime.GOOS == "linux" {
		appResourcesDir = filepath.Join(appFolder, "resources")
		appExePath = filepath.Join(appFolder, "claude-desktop")
		return
	}
	appResourcesDir = filepath.Join(appFolder, "Claude.app", "Contents", "Resources")
	appExePath = filepath.Join(appFolder, "Claude.app", "Contents", "MacOS", "Claude")
}

// prepareInstallDir is a no-op on non-Windows platforms.
func prepareInstallDir() error {
	return nil
}

// CoworkServiceExists is Windows-only; on other platforms report "present" so the shared
// launcher flow never tries to register a service.
func CoworkServiceExists() bool {
	return true
}

func finalizePatches() error {
	// This file also builds for Linux, which has no Claude.app and no codesign.
	if runtime.GOOS != "darwin" {
		return nil
	}

	// Info.plist is covered by the code signature, so the integrity hash has to be
	// final before signing.
	if err := updateAsarIntegrity(); err != nil {
		return err
	}
	return signApp(filepath.Join(AppFolder, "Claude.app"))
}

// updateAsarIntegrity records the repacked app.asar's header hash in Info.plist, which
// is where Electron reads the expected value from on macOS.
//
// Every failure here is fatal. A wrong value makes Electron abort with "Integrity check
// failed" on every single launch (issue #38), so a bundle with an unverified hash must
// never reach swapAppFolder — erroring out leaves the previous working install in place.
func updateAsarIntegrity() error {
	newHash, err := asar.HeaderHash(filepath.Join(appResourcesDir, "app.asar"))
	if err != nil {
		return fmt.Errorf("computing asar header hash: %v", err)
	}
	if !isHexHash(newHash) {
		return fmt.Errorf("computed asar header hash %q is not a 64-character lowercase hex digest", newHash)
	}

	plistPath := filepath.Join(AppFolder, "Claude.app", "Contents", "Info.plist")

	oldHash, viaPlistBuddy, err := readAsarIntegrityHash(plistPath)
	if err != nil {
		return err
	}
	if oldHash == newHash {
		// Re-patching an unchanged asar; nothing to do.
		fmt.Printf("Asar integrity hash already correct (%s)\n", newHash)
		return nil
	}

	fmt.Printf("Asar integrity hash: %s -> %s\n", oldHash, newHash)
	if viaPlistBuddy {
		err = plistBuddySetAsarHash(plistPath, newHash)
	} else {
		err = writePlistAsarHash(plistPath, newHash)
	}
	if err != nil {
		return err
	}

	// Read it back rather than trusting the write.
	got, _, err := readAsarIntegrityHash(plistPath)
	if err != nil {
		return fmt.Errorf("verifying Info.plist: %v", err)
	}
	if got != newHash {
		return fmt.Errorf("Info.plist asar integrity hash is %q after update, expected %q", got, newHash)
	}

	fmt.Println("Info.plist asar integrity hash updated")
	return nil
}

// readAsarIntegrityHash reads the current integrity hash, preferring the pure-Go XML
// path. Claude ships an XML Info.plist today; PlistBuddy is the fallback for the day
// that stops being true, so a format change costs us a slower path rather than a
// broken macOS build.
func readAsarIntegrityHash(plistPath string) (hash string, viaPlistBuddy bool, err error) {
	hash, err = readPlistAsarHash(plistPath)
	if err == nil {
		return hash, false, nil
	}
	if !errors.Is(err, errPlistHashNotFound) {
		return "", false, err
	}

	fmt.Println("Info.plist is not in the expected XML layout, falling back to PlistBuddy...")
	hash, buddyErr := plistBuddyGetAsarHash(plistPath)
	if buddyErr != nil {
		return "", true, fmt.Errorf("%v; %v", err, buddyErr)
	}
	return hash, true, nil
}

// signApp ad-hoc re-signs the bundle. Patching app.asar and Info.plist invalidates
// Claude's original signature, and macOS will not execute a bundle whose signature does
// not match, so a signing failure means an unlaunchable app and is fatal.
func signApp(appPath string) error {
	fmt.Println("Signing app with ad-hoc signature...")

	// An unsigned or already-stripped bundle is fine here, so ignore failures.
	if output, err := exec.Command("codesign", "--remove-signature", appPath).CombinedOutput(); err != nil || len(output) > 0 {
		fmt.Printf("Remove signature output: %s\n", strings.TrimSpace(string(output)))
	}

	output, err := exec.Command("codesign", "--force", "--deep", "--sign", "-", appPath).CombinedOutput()
	if err != nil {
		debugPause()
		return fmt.Errorf("ad-hoc signing %s: %v\n%s", appPath, err, string(output))
	}
	fmt.Println("App signed successfully")
	if len(output) > 0 {
		fmt.Printf("Signing output: %s\n", string(output))
	}
	return nil
}

func replacePlatformAppIcon() {
	// Linux uses the .desktop file + /usr/share/icons for its icon; the app
	// tree itself carries resources/icon.png which we leave untouched (as on
	// Windows, we don't modify the app's own icon so nothing breaks).
	if runtime.GOOS == "linux" {
		fmt.Println("  Skipping platform app icon replacement (Linux uses .desktop/share icons)")
		return
	}

	// Replace the app bundle icon
	icnsData, err := EmbeddedFS.ReadFile("resources/icons/app.icns")
	if err == nil {
		// electron.icns is in Claude.app/Contents/Resources/
		targetPath := filepath.Join(AppFolder, "Claude.app", "Contents", "Resources", "electron.icns")

		if err := os.WriteFile(targetPath, icnsData, 0644); err != nil {
			fmt.Printf("Warning: Could not replace app icon: %v\n", err)
		} else {
			fmt.Println("  Replaced electron.icns")
		}
	}
}

func GetLatestVersion() (string, string, error) {
	if runtime.GOOS == "linux" {
		return getLatestLinuxVersion()
	}

	fmt.Println("Getting latest version for OS: darwin")

	// Parse macOS manifest
	fmt.Printf("Fetching macOS manifest from: %s\n", macosReleasesURL)
	resp, err := http.Get(macosReleasesURL)
	if err != nil {
		return "", "", fmt.Errorf("fetching macOS manifest: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body for debugging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("reading macOS manifest body: %v", err)
	}

	var manifest MacOSManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		// Print first 500 chars for debugging
		debugLen := len(body)
		if debugLen > 500 {
			debugLen = 500
		}
		fmt.Printf("Failed to parse manifest. First %d chars: %s\n", debugLen, string(body[:debugLen]))
		return "", "", fmt.Errorf("parsing macOS manifest: %v", err)
	}

	// Get the current/latest release
	if manifest.CurrentRelease != "" {
		// Find the URL for the current release
		for _, release := range manifest.Releases {
			if release.Version == manifest.CurrentRelease {
				return release.Version, release.UpdateTo.URL, nil
			}
		}
	}

	// Fallback: if currentRelease is not set or not found, use the first release
	if len(manifest.Releases) > 0 {
		return manifest.Releases[0].Version, manifest.Releases[0].UpdateTo.URL, nil
	}

	return "", "", fmt.Errorf("no releases available in macOS manifest")
}

// linuxAptBase is the root of Anthropic's official Linux APT repository; the
// version is resolved from the binary-<arch> Packages index (same source the
// claude-desktop-debian project uses), and the .deb is downloaded from its pool.
const linuxAptBase = "https://downloads.claude.ai/claude-desktop/apt/stable"

// linuxAptSHA256 is the SHA-256 of the .deb selected by the most recent
// getLatestLinuxVersion call, taken verbatim from the signed APT Packages
// index. downloadAndExtractLinux insists the actual download matches it before
// extracting anything. It is populated in the same code path that produces the
// URL used by buildAndSwap, so it always describes the .deb being installed.
var linuxAptSHA256 string

// getLatestLinuxVersion resolves the newest Claude Desktop .deb for the host
// architecture from the official APT Packages index. Returns the version and
// the direct .deb download URL, and records the expected SHA-256 of that .deb.
func getLatestLinuxVersion() (string, string, error) {
	arch := HostArch() // "amd64" or "arm64"
	fmt.Printf("Getting latest version for OS: linux (%s)\n", arch)

	indexURL := fmt.Sprintf("%s/dists/stable/main/binary-%s/Packages", linuxAptBase, arch)
	fmt.Printf("Fetching Linux Packages index from: %s\n", indexURL)

	resp, err := http.Get(indexURL)
	if err != nil {
		return "", "", fmt.Errorf("fetching Linux Packages index: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("Linux Packages index returned status %d", resp.StatusCode)
	}

	newest, err := parseAptNewestClaudeDesk(resp.Body)
	if err != nil {
		return "", "", err
	}

	// The Filename is the pool path, e.g.
	// pool/main/c/claude-desktop/claude-desktop_1.40609.0_amd64.deb
	url := fmt.Sprintf("%s/%s", linuxAptBase, newest.filename)
	linuxAptSHA256 = newest.sha256
	fmt.Printf("Selected latest version: %s, URL: %s\n", newest.version, url)
	return newest.version, url, nil
}

type aptNewestEntry struct {
	version  string
	filename string
	sha256   string
}

// parseAptNewestClaudeDesk parses a Debian Packages index and returns the newest
// claude-desktop entry (by version) along with its pool filename and SHA-256.
// The .deb is then downloaded and its SHA-256 verified before extraction.
func parseAptNewestClaudeDesk(r io.Reader) (*aptNewestEntry, error) {
	var newest *aptNewestEntry
	cur := &aptNewestEntry{}

	flush := func() {
		if cur.version != "" && cur.filename != "" && cur.sha256 != "" {
			if newest == nil || versionCompare(cur.version, newest.version) > 0 {
				c := *cur
				newest = &c
			}
		}
		cur = &aptNewestEntry{}
	}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue // continuation lines, not needed
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Package":
			if value != "claude-desktop" {
				cur = &aptNewestEntry{} // different package, ignore
			}
		case "Version":
			cur.version = value
		case "Filename":
			cur.filename = value
		case "SHA256":
			cur.sha256 = value
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading Packages index: %v", err)
	}
	if newest == nil {
		return nil, fmt.Errorf("no claude-desktop entry found in Linux Packages index")
	}
	return newest, nil
}

// versionCompare compares two dotted numeric version strings (the scheme used by
// Claude's APT packages), returning -1, 0 or 1.
func versionCompare(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var da, db int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &da)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &db)
		}
		if da < db {
			return -1
		}
		if da > db {
			return 1
		}
	}
	return 0
}

// verifyDEBSHA256 computes the SHA-256 of the file at path and compares it
// (case-insensitively) against the digest the APT Packages index published for
// this package. An empty expected digest means the index gave us nothing to
// check against, which is also treated as a refusal-to-install.
//
// Note: matching the published SHA-256 detects corruption and casual tampering,
// but it is not itself proof the metadata is authentic — the digest is only as
// trustworthy as the channel that delivered it. The APT repository is served
// over HTTPS from Anthropic's domain and ships a signed InRelease/Packages set,
// so this check closes the loop between that signed metadata and the payload.
func verifyDEBSHA256(path, expected string) error {
	if expected == "" {
		return errors.New("APT Packages index did not list a SHA-256 for this .deb; refusing to install")
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening .deb for SHA-256 verification: %v", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing .deb: %v", err)
	}
	got := hex.EncodeToString(h.Sum(nil))

	if !strings.EqualFold(got, expected) {
		return fmt.Errorf(".deb SHA-256 mismatch (invalid download removed)\n  expected: %s\n  got:      %s", expected, got)
	}

	fmt.Printf("Verified .deb SHA-256: %s\n", got)
	return nil
}

func downloadAndExtract(version, downloadURL string) error {
	if runtime.GOOS == "linux" {
		return downloadAndExtractLinux(version, downloadURL)
	}

	newVersionZipName := fmt.Sprintf("Claude-%s.zip", version)

	// Define the download path based on whether we keep files or use temp
	var newVersionDownloadPath string
	if KeepDownloadedArchive {
		newVersionDownloadPath = utils.ResolvePath(newVersionZipName)
	} else {
		newVersionDownloadPath = utils.ResolvePath(newVersionZipName + ".tmp")
	}

	// Check if file already exists when KeepDownloadedArchive is enabled
	fileExists := false
	fullPath := utils.ResolvePath(newVersionZipName)
	if _, err := os.Stat(fullPath); err == nil {
		fileExists = true
	}

	if KeepDownloadedArchive && fileExists {
		fmt.Printf("Using existing file: %s\n", newVersionZipName)
	} else {
		// Download if file doesn't exist or if we're not keeping files
		fmt.Printf("Downloading from: %s\n", downloadURL)

		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("downloading: %v", err)
		}
		defer resp.Body.Close()

		// Use the already defined download path
		outFile, err := os.Create(newVersionDownloadPath)
		if err != nil {
			return fmt.Errorf("creating file: %v", err)
		}
		_, err = io.Copy(outFile, resp.Body)
		outFile.Close()
		if err != nil {
			return fmt.Errorf("saving file: %v", err)
		}
		fmt.Printf("Downloaded: %s\n", newVersionDownloadPath)
	}

	// Extract
	fmt.Println("Extracting...")
	os.RemoveAll(AppFolder)
	os.MkdirAll(AppFolder, 0755)

	zipReader, err := zip.OpenReader(newVersionDownloadPath)
	if err != nil {
		return fmt.Errorf("opening archive: %v", err)
	}
	// Don't defer close - we need to close before deleting temp file

	for _, f := range zipReader.File {
		// For macOS, keep the full .app bundle structure
		relativePath := f.Name

		if relativePath == "" {
			continue
		}

		path := filepath.Join(AppFolder, relativePath)

		// Handle PowerShell Compress-Archive's broken directory entries
		normalizedName := strings.ReplaceAll(f.Name, "\\", "/")
		isDirectory := f.FileInfo().IsDir() || (f.UncompressedSize64 == 0 && (strings.HasSuffix(normalizedName, "/") || strings.HasSuffix(f.Name, "\\")))

		if isDirectory {
			os.MkdirAll(path, 0755)
			continue
		}

		// Skip if path already exists as a directory (created by earlier MkdirAll)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			continue
		}

		os.MkdirAll(filepath.Dir(path), 0755)

		// Check if this is a symlink on macOS
		isSymlink := (f.ExternalAttrs>>16)&0170000 == 0120000

		if isSymlink {
			// Read the symlink target
			src, err := f.Open()
			if err != nil {
				continue
			}
			linkTarget, err := io.ReadAll(src)
			src.Close()

			if err == nil && len(linkTarget) > 0 {
				linkStr := string(linkTarget)
				// Create the symlink
				os.Remove(path) // Remove if exists
				if err := os.Symlink(linkStr, path); err == nil {
					fmt.Printf("Created symlink: %s -> %s\n", filepath.Base(path), linkStr)
				} else {
					fmt.Printf("Failed to create symlink %s: %v\n", path, err)
				}
				continue
			}
		}

		// Regular file extraction
		src, _ := f.Open()
		dst, _ := os.Create(path)
		io.Copy(dst, src)
		dst.Close()
		src.Close()
	}

	// Close the zip reader before attempting to delete temp file
	zipReader.Close()

	// macOS specific: Make sure the executable has execute permissions
	// Make the main executable executable
	claudeExec := filepath.Join(AppFolder, "Claude.app", "Contents", "MacOS", "Claude")
	if err := os.Chmod(claudeExec, 0755); err != nil {
		fmt.Printf("Warning: Could not set executable permissions: %v\n", err)
	}

	// Also make helper apps executable
	helpers := []string{
		"Claude Helper",
		"Claude Helper (GPU)",
		"Claude Helper (Plugin)",
		"Claude Helper (Renderer)",
	}
	for _, helper := range helpers {
		helperPath := filepath.Join(AppFolder, "Claude.app", "Contents", "Frameworks",
			helper+".app", "Contents", "MacOS", helper)
		if err := os.Chmod(helperPath, 0755); err != nil {
			// Don't warn for each one, they might not all exist
			continue
		}
	}

	// Also make chrome_crashpad_handler executable
	crashpadPath := filepath.Join(AppFolder, "Claude.app", "Contents", "Frameworks",
		"Electron Framework.framework", "Helpers", "chrome_crashpad_handler")
	if err := os.Chmod(crashpadPath, 0755); err != nil {
		// Don't warn, might not exist in all versions
	}

	// Delete ShipIt to prevent self-updates
	shipItPath := filepath.Join(AppFolder, "Claude.app", "Contents", "Frameworks", "Squirrel.framework", "Resources", "ShipIt")
	if err := os.Remove(shipItPath); err != nil && !os.IsNotExist(err) {
		fmt.Printf("Warning: Could not remove ShipIt: %v\n", err)
	} else {
		fmt.Println("Removed ShipIt to prevent self-updates")
	}

	// Delete the archive file only if KeepDownloadedArchive is false
	if !KeepDownloadedArchive {
		os.Remove(newVersionDownloadPath)
	} else {
		fmt.Printf("Keeping archive file: %s\n", newVersionZipName)
	}

	return nil
}

// downloadAndExtractLinux downloads the official Claude Desktop .deb and extracts
// the app tree (usr/lib/claude-desktop) into AppFolder, giving a flat Electron
// layout identical to the official install (claude-desktop + resources/ side by
// side). The .deb is an `ar` archive whose data payload (data.tar.xz or .zst) we
// unpack with the system `ar` + `tar` tools, which are present on the supported
// distros (binutils + GNU tar).
func downloadAndExtractLinux(version, downloadURL string) error {
	debName := fmt.Sprintf("Claude-%s.deb", version)
	var newVersionDownloadPath string
	if KeepDownloadedArchive {
		newVersionDownloadPath = utils.ResolvePath(debName)
	} else {
		newVersionDownloadPath = utils.ResolvePath(debName + ".tmp")
	}

	if _, err := os.Stat(newVersionDownloadPath); err != nil {
		fmt.Printf("Downloading from: %s\n", downloadURL)
		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("downloading .deb: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("downloading .deb: unexpected status %d", resp.StatusCode)
		}
		outFile, err := os.Create(newVersionDownloadPath)
		if err != nil {
			return fmt.Errorf("creating file: %v", err)
		}
		_, err = io.Copy(outFile, resp.Body)
		outFile.Close()
		if err != nil {
			return fmt.Errorf("saving .deb: %v", err)
		}
		fmt.Printf("Downloaded: %s\n", newVersionDownloadPath)
	} else {
		fmt.Printf("Using existing file: %s\n", newVersionDownloadPath)
	}

	// The .deb is never extracted before its SHA-256 matches what the signed
	// APT Packages index published for this version. A tampered or corrupted
	// download is deleted and installation aborts (no archive is touched).
	if err := verifyDEBSHA256(newVersionDownloadPath, linuxAptSHA256); err != nil {
		os.Remove(newVersionDownloadPath)
		return err
	}

	// Locate the data payload member inside the ar archive.
	listOutput, err := exec.Command("ar", "t", newVersionDownloadPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("listing .deb members with 'ar' (is binutils installed?): %v\n%s", err, string(listOutput))
	}
	dataMember := ""
	for _, line := range strings.Split(strings.TrimSpace(string(listOutput)), "\n") {
		if strings.HasPrefix(line, "data.tar.") {
			dataMember = line
			break
		}
	}
	if dataMember == "" {
		return fmt.Errorf(".deb has no data.tar.* member")
	}

	// Extract just that member to a temp file (this needs no root).
	extractRoot := utils.ResolvePath("deb-extract-tmp")
	os.RemoveAll(extractRoot)
	if err := os.MkdirAll(extractRoot, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(extractRoot)

	memberPath := filepath.Join(extractRoot, dataMember)
	memberFile, err := os.Create(memberPath)
	if err != nil {
		return err
	}
	arOutput, err := exec.Command("ar", "p", newVersionDownloadPath, dataMember).Output()
	if err != nil {
		memberFile.Close()
		return fmt.Errorf("extracting %s with 'ar': %v", dataMember, err)
	}
	if _, err := memberFile.Write(arOutput); err != nil {
		memberFile.Close()
		return err
	}
	memberFile.Close()

	fmt.Println("Extracting .deb data payload...")
	os.RemoveAll(AppFolder)
	if err := os.MkdirAll(AppFolder, 0755); err != nil {
		return err
	}

	// GNU tar auto-detects the compression (xz/zst) from the file's magic.
	if output, err := exec.Command("tar", "-xf", memberPath, "-C", extractRoot).CombinedOutput(); err != nil {
		return fmt.Errorf("unpacking data payload with 'tar' (is GNU tar installed?): %v\n%s", err, string(output))
	}

	srcTree := filepath.Join(extractRoot, "usr", "lib", "claude-desktop")
	if _, err := os.Stat(srcTree); err != nil {
		return fmt.Errorf("extracted .deb missing usr/lib/claude-desktop (layout changed?): %v", err)
	}
	if err := copyTree(srcTree, AppFolder); err != nil {
		return fmt.Errorf("staging app tree: %v", err)
	}

	// Make sure the main executable and helpers are executable.
	execs := []string{"claude-desktop", "chrome-sandbox", "chrome_crashpad_handler",
		filepath.Join("resources", "chrome-native-host"),
		filepath.Join("resources", "cowork-linux-helper"),
		filepath.Join("resources", "virtiofsd")}
	for _, rel := range execs {
		p := filepath.Join(AppFolder, rel)
		if _, err := os.Stat(p); err == nil {
			os.Chmod(p, 0755)
		}
	}

	if !KeepDownloadedArchive {
		os.Remove(newVersionDownloadPath)
	}
	return nil
}

// copyTree recursively copies the directory rooted at src into dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			os.Remove(target)
			return os.Symlink(link, target)
		}
		return copyFileMode(path, target, info.Mode())
	})
}

// copyFileMode copies a single file preserving its mode.
func copyFileMode(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
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
	return os.Chmod(dst, mode)
}

// through CFPropertyList, so it reads and writes binary plists too.
const plistBuddyPath = "/usr/libexec/PlistBuddy"

// asarIntegrityKeyPath addresses the hash in PlistBuddy's ":"-separated syntax. The key
// is literally "Resources/app.asar" — the dot is why plutil, whose key paths are
// dot-separated, is not used here.
const asarIntegrityKeyPath = ":ElectronAsarIntegrity:Resources/app.asar:hash"

func plistBuddyGetAsarHash(plistPath string) (string, error) {
	output, err := exec.Command(plistBuddyPath, "-c", "Print "+asarIntegrityKeyPath, plistPath).CombinedOutput()
	value := strings.TrimSpace(string(output))
	// PlistBuddy exits 0 on some -c errors, so the message matters as much as the code.
	if err != nil || value == "" || strings.Contains(value, "Does Not Exist") {
		return "", fmt.Errorf("PlistBuddy could not read %s from %s: %v (%s)", asarIntegrityKeyPath, plistPath, err, value)
	}
	return value, nil
}

func plistBuddySetAsarHash(plistPath, newHash string) error {
	output, err := exec.Command(plistBuddyPath,
		"-c", "Set "+asarIntegrityKeyPath+" "+newHash,
		"-c", "Save",
		plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("PlistBuddy could not set %s: %v (%s)", asarIntegrityKeyPath, err, strings.TrimSpace(string(output)))
	}
	return nil
}
