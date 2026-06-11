//go:build !windows

package patcher

import (
	"archive/zip"
	"bytes"
	"claude-webext-patcher/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func initPaths() {
	AppFolder = utils.ResolvePath(appFolderName)
	installBaseDir = utils.ResolvePath(".")
	appResourcesDir = filepath.Join(AppFolder, "Claude.app", "Contents", "Resources")
	appExePath = filepath.Join(AppFolder, "Claude.app", "Contents", "MacOS", "Claude")
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
	// Ad-hoc sign on macOS after asar modifications
	fmt.Println("Signing app with ad-hoc signature...")
	appPath := filepath.Join(AppFolder, "Claude.app")

	cmd := exec.Command("codesign", "--remove-signature", appPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Remove signature output: %s\n", string(output))
		// Ignore errors, might not be signed
	} else if len(output) > 0 {
		fmt.Printf("Remove signature output: %s\n", string(output))
	}

	cmd = exec.Command("codesign", "--force", "--deep", "--sign", "-", appPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Warning: Could not sign app: %v\n%s\n", err, string(output))
		debugPause()
	} else {
		fmt.Printf("App signed successfully\n")
		if len(output) > 0 {
			fmt.Printf("Signing output: %s\n", string(output))
		}
	}

	// macOS: capture hash mismatch, patch Info.plist, re-sign
	fmt.Println("Capturing hash mismatch...")
	expectedHash, actualHash, err := captureHashMismatch()
	if err != nil {
		return fmt.Errorf("capturing hash: %v", err)
	}

	fmt.Printf("Expected hash: %s\n", expectedHash)
	fmt.Printf("Actual hash: %s\n", actualHash)

	fmt.Println("Patching exe...")
	if err := replaceHashInExe(expectedHash, actualHash); err != nil {
		return fmt.Errorf("patching exe: %v", err)
	}

	// Ad-hoc sign on macOS after all modifications
	fmt.Println("Signing app with ad-hoc signature...")

	cmd = exec.Command("codesign", "--remove-signature", appPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Remove signature output: %s\n", string(output))
	} else if len(output) > 0 {
		fmt.Printf("Remove signature output: %s\n", string(output))
	}

	cmd = exec.Command("codesign", "--force", "--deep", "--sign", "-", appPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Warning: Could not sign app: %v\n%s\n", err, string(output))
		debugPause()
	} else {
		fmt.Printf("App signed successfully\n")
		if len(output) > 0 {
			fmt.Printf("Signing output: %s\n", string(output))
		}
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

func downloadAndExtract(version, downloadURL string) error {
	newVersionZipName := fmt.Sprintf("Claude-%s.zip", version)

	// Define the download path based on whether we keep files or use temp
	var newVersionDownloadPath string
	if KeepNupkgFiles {
		newVersionDownloadPath = utils.ResolvePath(newVersionZipName)
	} else {
		newVersionDownloadPath = utils.ResolvePath(newVersionZipName + ".tmp")
	}

	// Check if file already exists when KeepNupkgFiles is enabled
	fileExists := false
	fullPath := utils.ResolvePath(newVersionZipName)
	if _, err := os.Stat(fullPath); err == nil {
		fileExists = true
	}

	if KeepNupkgFiles && fileExists {
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

	// Delete the archive file only if KeepNupkgFiles is false
	if !KeepNupkgFiles {
		os.Remove(newVersionDownloadPath)
	} else {
		fmt.Printf("Keeping archive file: %s\n", newVersionZipName)
	}

	return nil
}

func captureHashMismatch() (string, string, error) {
	cmd := exec.Command(appExePath)
	output, _ := cmd.CombinedOutput()

	// Parse the error output for the hashes
	// Looking for pattern: "Integrity check failed for asar archive (EXPECTED vs ACTUAL)"
	outputStr := string(output)
	fmt.Println(outputStr)
	if strings.Contains(outputStr, "Integrity check failed") {
		// Extract the hashes using a simple string parse
		start := strings.Index(outputStr, "(")
		end := strings.Index(outputStr, ")")
		if start != -1 && end != -1 {
			hashPart := outputStr[start+1 : end]
			parts := strings.Split(hashPart, " vs ")
			if len(parts) == 2 {
				return parts[0], parts[1], nil
			}
		}
	}

	return "", "", fmt.Errorf("could not parse hash mismatch")
}

func replaceHashInExe(oldHash, newHash string) error {
	// On macOS, the hash is in Info.plist
	plistPath := filepath.Join(AppFolder, "Claude.app", "Contents", "Info.plist")

	// Read the plist
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return fmt.Errorf("reading Info.plist: %v", err)
	}

	// Replace the hash
	replaced := bytes.Replace(data, []byte(oldHash), []byte(newHash), 1)
	if bytes.Equal(replaced, data) {
		return fmt.Errorf("hash not found in Info.plist")
	}

	// Write back
	return os.WriteFile(plistPath, replaced, 0644)
}
