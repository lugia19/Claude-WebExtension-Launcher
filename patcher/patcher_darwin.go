package patcher

import (
	"archive/zip"
	"claude-webext-patcher/asar"
	"claude-webext-patcher/utils"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const macosReleasesURL = "https://downloads.claude.ai/releases/darwin/universal/RELEASES.json"

type MacOSManifest struct {
	CurrentRelease string `json:"currentRelease"`
	Releases       []struct {
		Version  string `json:"version"`
		UpdateTo struct {
			URL string `json:"url"`
		} `json:"updateTo"`
	} `json:"releases"`
}

// setAppPaths points the app-folder globals at appFolder. buildAndSwap uses this to
// build a patched install in a staging folder before swapping it into place.
func setAppPaths(appFolder string) {
	AppFolder = appFolder
	appResourcesDir = filepath.Join(appFolder, "Claude.app", "Contents", "Resources")
	appExePath = filepath.Join(appFolder, "Claude.app", "Contents", "MacOS", "Claude")
}

func finalizePatches() error {
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

// Prefetch downloads the Claude zip and returns its path.
func Prefetch(version, url string) (string, error) {
	path := utils.ResolvePath(fmt.Sprintf("Claude-%s.zip", version))
	if err := downloadFile(url, path); err != nil {
		return "", err
	}
	return path, nil
}

// downloadAndExtract extracts the zip the launcher downloaded. The worker runs as the
// same user, so it can use the file as-is.
func downloadAndExtract(version, downloadURL string) error {
	newVersionDownloadPath := PrefetchedPackage
	if newVersionDownloadPath == "" || PrefetchedVersion != version {
		return fmt.Errorf("no downloaded package for Claude %s", version)
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
		if err := utils.ExtractZipFile(f, path); err != nil {
			zipReader.Close()
			return fmt.Errorf("extracting %s: %v", f.Name, err)
		}
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

	return nil
}

// plistBuddyPath is macOS's built-in plist editor. Unlike the XML splice it goes
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
