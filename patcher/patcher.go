package patcher

import (
	"archive/zip"
	"bytes"
	"claude-webext-patcher/utils"
	"crypto/sha256"
	"crypto/sha512"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// EmbeddedFS is the embedded filesystem from the main package
var EmbeddedFS embed.FS

const (
	windowsReleasesURL = "https://storage.googleapis.com/osprey-downloads-c02f6a0d-347c-492b-a752-3e0651722e97/nest-win-x64/RELEASES"
	macosReleasesURL   = "https://storage.googleapis.com/osprey-downloads-c02f6a0d-347c-492b-a752-3e0651722e97/nest/update_manifest.json"
	appFolderName      = "app-latest"
	KeepNupkgFiles     = false
)

type MacOSManifest struct {
	CurrentRelease string `json:"currentRelease"`
	Releases       []struct {
		Version  string `json:"version"`
		UpdateTo struct {
			URL string `json:"url"`
		} `json:"updateTo"`
	} `json:"releases"`
}

type Patch struct {
	Files []string
	Func  func(content []byte) []byte
}

var supportedVersions = map[string][]Patch{
	"0.12.55": {
		{
			Files: []string{".vite/build/index-BZRfNpEg.js", ".vite/build/index-DyHP6ri_.js"},
			Func:  patch_0_12_55,
		},
	},
}

var (
	AppFolder       = utils.ResolvePath(appFolderName)
	appResourcesDir string
	appExePath      string
	// SkipPatches controls whether we skip modifying app.asar and related injections.
	// Can be toggled via CLI (set by main) or by environment variable CLWEL_NO_INJECT.
	SkipPatches     bool
	// ForceReinstall forces re-download/extraction even if the newest supported version
	// is already installed. Can be toggled via CLI (set by main) or env CLWEL_FORCE_REINSTALL.
	ForceReinstall  bool
)

func init() {
	if runtime.GOOS == "darwin" {
		appResourcesDir = filepath.Join(AppFolder, "Claude.app", "Contents", "Resources")
		appExePath = filepath.Join(AppFolder, "Claude.app", "Contents", "MacOS", "Claude")
	} else {
		appResourcesDir = filepath.Join(AppFolder, "resources")
		appExePath = filepath.Join(AppFolder, "claude.exe")
	}
}

// Patch functions
func readInjection(version, filename string) (string, error) {
	// Must use forward slashes for embed.FS, not filepath.Join
	injectionPath := "resources/injections/" + version + "/" + filename
	content, err := EmbeddedFS.ReadFile(injectionPath)
	if err != nil {
		return "", fmt.Errorf("could not load injection %s for version %s: %v", filename, version, err)
	}
	return string(content), nil
}

func readCombinedInjection(version string, filenames []string) (string, error) {
	var combined strings.Builder

	for i, filename := range filenames {
		content, err := readInjection(version, filename)
		if err != nil {
			return "", err
		}

		// Add the content
		combined.WriteString(content)

		// Add newlines between files for clarity
		if i < len(filenames)-1 {
			combined.WriteString("\n\n")
		}
	}

	return combined.String(), nil
}

func patch_0_12_55(content []byte) []byte {
	contentStr := string(content)

	// Load all injection files
	injectionFiles := []string{
		"extension_loader.js",
		"alarm_polyfill.js",
		"notification_polyfill.js",
		"tabevents_polyfill.js",
	}

	injection, err := readCombinedInjection("0.12.55", injectionFiles)
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
		return content
	}

	// First injection point - inside use() function
	returnPattern := `return a(), i(), e.on("resize"`
	if idx := strings.Index(contentStr, returnPattern); idx != -1 {
		contentStr = contentStr[:idx] + "\n" + injection + "\n" + contentStr[idx:]
	}

	// Second injection point - add chrome-extension: to the array
	gxPattern := `gX = ["devtools:", "file:"]`
	gxReplacement := `gX = ["devtools:", "file:", "chrome-extension:"]`
	contentStr = strings.Replace(contentStr, gxPattern, gxReplacement, 1)

	return []byte(contentStr)
}

var (
	asarCmd       string
	jsBeautifyCmd string
)

func ensureTools() error {
	// Check if node exists and get version
	cmd := exec.Command("node", "--version")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error: Node.js not found. Please install Node.js first.")
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		return fmt.Errorf("Node.js not found")
	}

	// Parse Node version (format: v22.0.0)
	versionStr := strings.TrimSpace(string(output))
	versionStr = strings.TrimPrefix(versionStr, "v")
	versionParts := strings.Split(versionStr, ".")
	majorVersion := 0
	if len(versionParts) > 0 {
		fmt.Sscanf(versionParts[0], "%d", &majorVersion)
	}

	fmt.Printf("Found Node.js %s\n", versionStr)

	// Set tool paths
	nodeModulesPath := utils.ResolvePath(filepath.Join("node_modules", ".bin"))
	asarCmd = filepath.Join(nodeModulesPath, "asar")
	jsBeautifyCmd = filepath.Join(nodeModulesPath, "js-beautify")

	if runtime.GOOS == "windows" {
		asarCmd += ".ps1"
		jsBeautifyCmd += ".ps1"
	}

	// Install locally if needed
	if _, err := os.Stat(asarCmd); os.IsNotExist(err) {
		fmt.Println("Installing tools locally...")

		// Choose asar package based on Node version
		asarPackage := "asar"
		if majorVersion >= 22 {
			asarPackage = "@electron/asar"
		}

		installDir := utils.ResolvePath(".")
		cmd := exec.Command("npm", "install", "--prefix", installDir, "--no-save", asarPackage, "js-beautify")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install tools: %v", err)
		}
	}

	return nil
}

func getLatestSupportedVersion() (string, string, error) {
	// Get supported supportedVersionsList sorted newest first
	supportedVersionsList := make([]string, 0, len(supportedVersions))
	for v := range supportedVersions {
		supportedVersionsList = append(supportedVersionsList, v)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(supportedVersionsList)))

	if runtime.GOOS == "darwin" {
		// Parse macOS manifest
		resp, err := http.Get(macosReleasesURL)
		if err != nil {
			return "", "", fmt.Errorf("fetching macOS manifest: %v", err)
		}
		defer resp.Body.Close()

		var manifest MacOSManifest
		if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
			return "", "", fmt.Errorf("parsing macOS manifest: %v", err)
		}

		// Build a map of available supportedVersionsList to their URLs
		availableVersions := make(map[string]string)
		for _, release := range manifest.Releases {
			availableVersions[release.Version] = release.UpdateTo.URL
		}

		// Find newest supported version that's available
		for _, version := range supportedVersionsList {
			if url, exists := availableVersions[version]; exists {
				return version, url, nil
			}
		}
		return "", "", fmt.Errorf("no supported supportedVersionsList available in macOS manifest")
	} else {
		// Windows - use existing RELEASES logic
		resp, err := http.Get(windowsReleasesURL)
		if err != nil {
			return "", "", fmt.Errorf("fetching Windows releases: %v", err)
		}
		defer resp.Body.Close()

		releasesText, _ := io.ReadAll(resp.Body)

		// Find newest supported version
		for _, version := range supportedVersionsList {
			filename := fmt.Sprintf("AnthropicClaude-%s-full.nupkg", version)
			if strings.Contains(string(releasesText), filename) {
				downloadURL := strings.Replace(windowsReleasesURL, "RELEASES", filename, 1)
				return version, downloadURL, nil
			}
		}
		return "", "", fmt.Errorf("no supported supportedVersionsList available in Windows releases")
	}
}

func downloadAndExtract(version, downloadURL string) error {
	var newVersionZipName string
	if runtime.GOOS == "darwin" {
		newVersionZipName = fmt.Sprintf("Claude-%s.zip", version)
	} else {
		newVersionZipName = fmt.Sprintf("AnthropicClaude-%s-full.nupkg", version)
	}

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
		var relativePath string

		if runtime.GOOS == "darwin" {
			// For macOS, keep the full .app bundle structure
			relativePath = f.Name
		} else {
			// Windows - only extract files from lib/net45/
			if !strings.HasPrefix(f.Name, "lib/net45/") {
				continue
			}
			relativePath = strings.TrimPrefix(f.Name, "lib/net45/")
		}

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
		isSymlink := runtime.GOOS == "darwin" &&
			(f.ExternalAttrs>>16)&0170000 == 0120000

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

	// Delete the archive file only if KeepNupkgFiles is false
	if !KeepNupkgFiles {
		os.Remove(newVersionDownloadPath)
		fmt.Println("Removed temporary archive file")
	} else {
		fmt.Printf("Keeping archive file: %s\n", newVersionZipName)
	}

	// macOS specific: Make sure the executable has execute permissions
	if runtime.GOOS == "darwin" {
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
	}

	return nil
}

func EnsurePatched() error {
	// Resolve environment variable fallbacks
	skip := SkipPatches || os.Getenv("CLWEL_NO_INJECT") != ""
	force := ForceReinstall || os.Getenv("CLWEL_FORCE_REINSTALL") != ""

	// Only require Node/tools when we intend to patch/inject
	if !skip {
		if err := ensureTools(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	}

	// Get current version
	currentVersion := ""
	claudeVersionFile := filepath.Join(AppFolder, "claude-version.txt")
	if data, err := os.ReadFile(claudeVersionFile); err == nil {
		currentVersion = strings.TrimSpace(string(data))
		fmt.Printf("Current version: %s\n", currentVersion)
	}

	// Get newest supported version and download URL
	newestVersion, downloadURL, err := getLatestSupportedVersion()
	if err != nil {
		return err
	}

	fmt.Printf("Newest supported version: %s\n", newestVersion)

	if currentVersion != newestVersion || force {
		fmt.Printf("Updating to %s...\n", newestVersion)

		if err := downloadAndExtract(newestVersion, downloadURL); err != nil {
			return err
		}

		// Write version
		os.WriteFile(claudeVersionFile, []byte(newestVersion), 0644)

		if skip {
			fmt.Println("Safe mode: skipping asar injection and hash patching.")
			finalizeApp()
		} else {
			// Apply patches
			if err := applyPatches(newestVersion); err != nil {
				return fmt.Errorf("applying patches: %v", err)
			}
		}
	} else if skip {
		// Even if up-to-date, allow finalize step to ensure correct signing/quarantine
		fmt.Println("Safe mode: up-to-date. Skipping injection; finalizing existing install.")
		finalizeApp()
	}

	return nil
}

func applyPatches(version string) error {
	patches, ok := supportedVersions[version]
	if !ok || len(patches) == 0 {
		return nil
	}

	fmt.Println("Applying patches...")
	if err := replaceIcons(); err != nil {
		fmt.Printf("Warning: Could not replace icons: %v\n", err)
		// Don't fail the whole process if icons can't be replaced
	}

	asarPath := filepath.Join(appResourcesDir, "app.asar")
	tempDir := utils.ResolvePath("asar-temp")

	// Unpack asar
	fmt.Println("Unpacking asar...")
	fmt.Printf("Running command: %s\n", asarCmd)
	fmt.Printf("Arguments: extract %s %s\n", asarPath, tempDir)

	var cmd *exec.Cmd
	// On Windows, .ps1 files need to be run through PowerShell
	if runtime.GOOS == "windows" && strings.HasSuffix(asarCmd, ".ps1") {
		fmt.Println("Using PowerShell for Windows .ps1 file")
		cmd = exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", asarCmd, "extract", asarPath, tempDir)
	} else {
		cmd = exec.Command(asarCmd, "extract", asarPath, tempDir)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Command failed with error: %v\n", err)
		fmt.Printf("Output: %s\n", string(output))
		return fmt.Errorf("unpacking asar: %v\nOutput: %s", err, string(output))
	}
	fmt.Printf("Unpacking successful\n")
	defer os.RemoveAll(tempDir)

	// Apply patches
	for i, patch := range patches {
		fmt.Printf("Applying patch %d/%d...\n", i+1, len(patches))

		// Try each file for this patch until one exists and successfully applies
		patchApplied := false
		for _, file := range patch.Files {
			filePath := filepath.Join(tempDir, file)
			content, err := os.ReadFile(filePath)
			if err != nil {
				fmt.Printf("Skipping %s: %v\n", file, err)
				continue // Try next file
			}

			fmt.Printf("Found file: %s\n", file)

			if strings.HasSuffix(file, ".js") {
				// Check if already beautified
				if !bytes.Contains(content, []byte("/* CLAUDE-MANAGER-BEAUTIFIED */")) {
					// Beautify the file
					var beautifyCmd *exec.Cmd
					// On Windows, .ps1 files need to be run through PowerShell
					if runtime.GOOS == "windows" && strings.HasSuffix(jsBeautifyCmd, ".ps1") {
						beautifyCmd = exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", jsBeautifyCmd, filePath, "-o", filePath)
					} else {
						beautifyCmd = exec.Command(jsBeautifyCmd, filePath, "-o", filePath)
					}
					if err := beautifyCmd.Run(); err != nil {
						fmt.Printf("Warning: Could not beautify %s: %v\n", file, err)
					} else {
						// Add marker comment
						beautifiedContent, _ := os.ReadFile(filePath)
						markedContent := append([]byte("/* CLAUDE-MANAGER-BEAUTIFIED */\n"), beautifiedContent...)
						os.WriteFile(filePath, markedContent, 0644)

						// Re-read for patching
						content = markedContent
					}
				}
			}

			// Print first 100 chars for debugging
			debugLen := len(content)
			if debugLen > 100 {
				debugLen = 100
			}
			fmt.Printf("Patching %s (first %d chars): %s...\n", file, debugLen, string(content[:debugLen]))

			newContent := patch.Func(content)
			if err := os.WriteFile(filePath, newContent, 0644); err != nil {
				fmt.Printf("Failed to write %s: %v\n", file, err)
				continue // Try next file
			}

			fmt.Printf("Successfully applied patch to %s\n", file)
			patchApplied = true
			break // Move to next patch
		}

		if !patchApplied {
			// Fallback: hashed Vite filenames sometimes change. Try scanning .vite/build for index-*.js
			// and apply the patch if it results in a content change.
			viteDir := filepath.Join(tempDir, ".vite", "build")
			entries, err := os.ReadDir(viteDir)
			if err == nil {
				for _, e := range entries {
					name := e.Name()
					if e.IsDir() || !strings.HasPrefix(name, "index-") || !strings.HasSuffix(name, ".js") {
						continue
					}
					candidate := filepath.Join(viteDir, name)
					content, err := os.ReadFile(candidate)
					if err != nil {
						continue
					}

					// Attempt patch; only write back if content actually changes
					newContent := patch.Func(content)
					if !bytes.Equal(newContent, content) {
						if err := os.WriteFile(candidate, newContent, 0644); err != nil {
							fmt.Printf("Failed to write fallback file %s: %v\n", name, err)
							continue
						}
						fmt.Printf("Successfully applied patch via fallback to %s\n", candidate)
						patchApplied = true
						break
					}
				}
			}

			if !patchApplied {
				return fmt.Errorf("patch %d failed: none of the target files could be patched", i+1)
			}
		}
	}

	// Backup original
	os.Rename(asarPath, asarPath+".backup")

	// Repack asar
	fmt.Println("Repacking asar...")
	fmt.Printf("Running command: %s\n", asarCmd)
	fmt.Printf("Arguments: pack %s %s\n", tempDir, asarPath)

	// On Windows, .ps1 files need to be run through PowerShell
	if runtime.GOOS == "windows" && strings.HasSuffix(asarCmd, ".ps1") {
		fmt.Println("Using PowerShell for Windows .ps1 file")
		cmd = exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", asarCmd, "pack", tempDir, asarPath)
	} else {
		cmd = exec.Command(asarCmd, "pack", tempDir, asarPath)
	}
	output2, err2 := cmd.CombinedOutput()
	if err2 != nil {
		fmt.Printf("Command failed with error: %v\n", err)
		fmt.Printf("Output: %s\n", string(output2))
		os.Rename(asarPath+".backup", asarPath) // Restore on failure
		return fmt.Errorf("repacking asar: %v\nOutput: %s", err, string(output2))
	}
	fmt.Printf("Repacking successful\n")

	// Fix the hash in the exe
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

	// Finalize the app (sign and clear quarantine on macOS)
	finalizeApp()

	fmt.Println("Patches applied successfully!")
	return nil
}

func captureHashMismatch() (string, string, error) {
    // On macOS, the asar integrity is stored in Info.plist. Running the GUI app won't reliably
    // print integrity errors to stdout. Instead, parse Info.plist for the asar integrity entry,
    // compute the hash of app.asar, and return (expected, actual).
    // Note: Electron expects the SHA256 over the ASAR HEADER only, not the whole file,
    // specifically the raw header string bytes. See Electron "ASAR Integrity" docs.
	if runtime.GOOS == "darwin" {
		return captureHashFromPlist()
	}

	// Non-macOS fallback: run the executable and parse the integrity error output.
	cmd := exec.Command(appExePath)
	output, _ := cmd.CombinedOutput()

	// Parse the error output for the hashes
	// Looking for pattern: "Integrity check failed for asar archive (EXPECTED vs ACTUAL)"
	outputStr := string(output)
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

func captureHashFromPlist() (string, string, error) {
	plistPath := filepath.Join(AppFolder, "Claude.app", "Contents", "Info.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return "", "", fmt.Errorf("reading Info.plist: %v", err)
	}

	// Try to locate the asar integrity within the plist (works for XML and often binary too).
	s := string(data)
	lower := strings.ToLower(s)
	idx := strings.Index(lower, "asarintegrity")
	if idx == -1 {
		// Some packagers use ElectronAsarIntegrity; be generous and search 'electron' + 'asarintegrity'
		idx = strings.Index(lower, "electronasarintegrity")
	}
	if idx == -1 {
		return "", "", fmt.Errorf("asar integrity entry not found in Info.plist")
	}

	// Search within a window around the integrity entry for algorithm and hash fields.
	start := idx - 200
	if start < 0 {
		start = 0
	}
	end := idx + 1200
	if end > len(s) {
		end = len(s)
	}
	window := s[start:end]

	algo := ""
	expected := ""
	// JSON-like form
	algoJSON := regexp.MustCompile(`(?i)"algorithm"\s*:\s*"([^"]+)"`)
	hashJSON := regexp.MustCompile(`(?i)"hash"\s*:\s*"([A-Za-z0-9+\/=]+|[A-Fa-f0-9]{64,128})"`)

	if m := algoJSON.FindStringSubmatch(window); len(m) == 2 {
		algo = m[1]
	}
	if m := hashJSON.FindStringSubmatch(window); len(m) == 2 {
		expected = m[1]
	}

	// XML form fallback
	if expected == "" {
		algoXML := regexp.MustCompile(`(?i)<key>\s*algorithm\s*</key>\s*<string>\s*([^<]+)\s*</string>`)
		hashXML := regexp.MustCompile(`(?i)<key>\s*hash\s*</key>\s*<string>\s*([^<]+)\s*</string>`)
		if m := algoXML.FindStringSubmatch(window); len(m) == 2 {
			algo = strings.TrimSpace(m[1])
		}
		if m := hashXML.FindStringSubmatch(window); len(m) == 2 {
			expected = strings.TrimSpace(m[1])
		}
	}
	if expected == "" {
		return "", "", fmt.Errorf("could not locate expected asar hash in Info.plist")
	}
	if algo == "" {
		// Default to SHA512 if algorithm is not explicitly present
		algo = "SHA512"
	}

	// Compute actual HEADER hash of app.asar (not the whole file). Electron expects the
	// SHA256 of the raw header, as documented in "ASAR Integrity". Use @electron/asar's
	// getRawHeader via Node to compute the correct value.
	actual, err := computeAsarHeaderHashHex()
	if err != nil {
		return "", "", fmt.Errorf("computing asar header hash: %v", err)
	}
	return expected, actual, nil
}

// computeAsarHeaderHashHex computes the SHA256 hash (hex) of the raw ASAR header
// using the @electron/asar library's getRawHeader helper via Node.js.
// Important:
//   - Electron validates the header string (getRawHeader(...).headerString), not the
//     entire archive. We therefore hash exactly those bytes.
//   - We resolve the module via Node's createRequire() pointed at our Application Support
//     node_modules path (see utils.ResolvePath). We first try '@electron/asar' (Node >= 22)
//     and fall back to 'asar' for older installs.
func computeAsarHeaderHashHex() (string, error) {
	asarPath := filepath.Join(appResourcesDir, "app.asar")

	// Resolve the node_modules base path for module resolution
	moduleBasePath, err := getNodeModulesBasePath()
	if err != nil {
		return "", err
	}

	// Write a small temporary Node script to compute the header hash
	script := `const crypto=require('node:crypto');
const {createRequire}=require('node:module');
const p=process.argv[2];
const nm=process.argv[3];
const req=createRequire(nm.endsWith('/')? nm : nm + '/');
let asar;
try { asar = req('@electron/asar'); } catch(_) { asar = req('asar'); }
try{
  const result=asar.getRawHeader(p);
  const raw=(result && result.headerString) ? result.headerString : result;
  const h=crypto.createHash('sha256').update(typeof raw === 'string' ? Buffer.from(raw) : raw).digest('hex');
  process.stdout.write(h);
}catch(e){
  console.error('ERR:'+e.message);
  process.exit(1);
}`

	tmpFile := filepath.Join(os.TempDir(), "clwel_compute_header_hash.js")
	if err := os.WriteFile(tmpFile, []byte(script), 0644); err != nil {
		return "", fmt.Errorf("writing temp node script: %v", err)
	}
	defer os.Remove(tmpFile)

	cmd := exec.Command("node", tmpFile, asarPath, moduleBasePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("node compute header hash failed: %v\nOutput: %s", err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// getNodeModulesBasePath resolves the node_modules base directory for require() lookups.
func getNodeModulesBasePath() (string, error) {
	nodeModulesPath := utils.ResolvePath("node_modules")
	if info, err := os.Stat(nodeModulesPath); err == nil && info.IsDir() {
		return nodeModulesPath, nil
	}
	return "", fmt.Errorf("node_modules not found; ensure tools are installed")
}

func computeAsarHashBytes(algorithm string) ([]byte, error) {
	asarPath := filepath.Join(appResourcesDir, "app.asar")
	f, err := os.Open(asarPath)
	if err != nil {
		return nil, fmt.Errorf("opening app.asar: %v", err)
	}
	defer f.Close()

	// Choose hash algorithm; default SHA512
	algLower := strings.ToLower(algorithm)
	if strings.Contains(algLower, "256") {
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return nil, fmt.Errorf("hashing app.asar (sha256): %v", err)
		}
		sum := h.Sum(nil)
		return sum, nil
	}
	// Fallback to sha512
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("hashing app.asar (sha512): %v", err)
	}
	sum := h.Sum(nil)
	return sum, nil
}

func encodeToMatch(exemplar string, b []byte) string {
	isHex := regexp.MustCompile(`^[A-Fa-f0-9]{64,128}$`).MatchString(exemplar)
	if isHex {
		const hexdigits = "0123456789abcdef"
		out := make([]byte, len(b)*2)
		for i, v := range b {
			out[i*2] = hexdigits[v>>4]
			out[i*2+1] = hexdigits[v&0x0f]
		}
		return string(out)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func replaceHashInExe(oldHash, newHash string) error {
	if runtime.GOOS == "darwin" {
		// On macOS, the hash can appear in multiple Info.plists (app + helpers/frameworks).
		// Walk all Info.plists under Contents/ and replace occurrences of the old hash
		// within ElectronAsarIntegrity entries that reference Resources/app.asar.
		contentsRoot := filepath.Join(AppFolder, "Claude.app", "Contents")
		updated := 0
		walkErr := filepath.WalkDir(contentsRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Skip problematic entries but continue
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if filepath.Base(path) != "Info.plist" {
				return nil
			}

			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			// Only consider plists that define ElectronAsarIntegrity and reference app.asar
			if !bytes.Contains(data, []byte("ElectronAsarIntegrity")) || !bytes.Contains(data, []byte("Resources/app.asar")) {
				return nil
			}

			if !bytes.Contains(data, []byte(oldHash)) {
				return nil
			}

			replaced := bytes.ReplaceAll(data, []byte(oldHash), []byte(newHash))
			if bytes.Equal(replaced, data) {
				return nil
			}
			if werr := os.WriteFile(path, replaced, 0644); werr != nil {
				fmt.Printf("Warning: failed updating %s: %v\n", path, werr)
				return nil
			}
			fmt.Printf("Updated asar integrity hash in %s\n", path)
			updated++
			return nil
		})
		if walkErr != nil {
			return fmt.Errorf("updating Info.plist files: %v", walkErr)
		}
		if updated == 0 {
			return fmt.Errorf("hash not found in any Info.plist files")
		}
		return nil
	} else {
		// Windows - hash is in the executable
		data, err := os.ReadFile(appExePath)
		if err != nil {
			return err
		}

		// Search for the hash as a string
		replaced := bytes.Replace(data, []byte(oldHash), []byte(newHash), 1)
		if bytes.Equal(replaced, data) {
			return fmt.Errorf("hash not found in executable")
		}

		// Write back
		return os.WriteFile(appExePath, replaced, 0755)
	}
}

func finalizeApp() {
	if runtime.GOOS != "darwin" {
		return
	}
	fmt.Println("Finalizing app (signing and clearing quarantine)...")
	appPath := filepath.Join(AppFolder, "Claude.app")

	// Remove existing signature first
	cmd := exec.Command("codesign", "--remove-signature", appPath)
	cmd.Run() // Ignore errors, might not be signed

	// Sign with ad-hoc signature
	cmd = exec.Command("codesign", "--force", "--deep", "--sign", "-", appPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Warning: Could not sign app: %v\n%s\n", err, string(output))
		// Continue anyway - might still work
	} else {
		fmt.Println("App signed successfully")
	}

	// Clear quarantine attributes recursively
	cmd = exec.Command("xattr", "-dr", "com.apple.quarantine", appPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Warning: Could not clear quarantine: %v\n%s\n", err, string(output))
	} else {
		fmt.Println("Cleared quarantine attributes on Claude.app")
	}
}

func replaceIcons() error {
	fmt.Println("Replacing icons...")

	// OS-specific exe/app icon replacement
	switch runtime.GOOS {
	case "windows":
		// Extract rcedit.exe to temp file
		rceditData, err := EmbeddedFS.ReadFile("resources/rcedit.exe")
		if err != nil {
			return fmt.Errorf("reading embedded rcedit.exe: %v", err)
		}

		tempRcedit := filepath.Join(os.TempDir(), "rcedit-temp.exe")
		if err := os.WriteFile(tempRcedit, rceditData, 0755); err != nil {
			return fmt.Errorf("writing temp rcedit.exe: %v", err)
		}
		defer os.Remove(tempRcedit)

		// Extract app.ico to temp file
		icoData, err := EmbeddedFS.ReadFile("resources/icons/app.ico")
		if err != nil {
			return fmt.Errorf("reading embedded app.ico: %v", err)
		}

		tempIco := filepath.Join(os.TempDir(), "app-temp.ico")
		if err := os.WriteFile(tempIco, icoData, 0644); err != nil {
			return fmt.Errorf("writing temp app.ico: %v", err)
		}
		defer os.Remove(tempIco)

		cmd := exec.Command(tempRcedit, appExePath, "--set-icon", tempIco)
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: Could not replace exe icon: %v\n", err)
		}
	case "darwin":
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

	// Copy other icons (works for all platforms)
	iconEntries, err := EmbeddedFS.ReadDir("resources/icons")
	if err != nil {
		return err
	}

	for _, entry := range iconEntries {
		if entry.IsDir() {
			continue
		}

		// Must use forward slashes for embed.FS
		iconPath := "resources/icons/" + entry.Name()
		dst := filepath.Join(appResourcesDir, entry.Name())

		fmt.Printf("  %s -> %s\n", entry.Name(), dst)

		input, err := EmbeddedFS.ReadFile(iconPath)
		if err != nil {
			return err
		}

		if err := os.WriteFile(dst, input, 0644); err != nil {
			return err
		}
	}

	return nil
}
