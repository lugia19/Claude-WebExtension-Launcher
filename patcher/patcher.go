package patcher

import (
	"archive/zip"
	"bytes"
	"claude-webext-patcher/utils"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
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
	// Generic patch that should work for most versions
	"generic": {
		{
			// Use pattern to match all index*.js files
			Files: []string{".vite/build/index*.js"},
			Func: func(content []byte) []byte {
				return patch_generic(content)
			},
		},
	},
	// Add version-specific overrides here when needed
	// Example:
	// "0.13.0": {
	//     {
	//         Files: []string{"specific-file.js"},
	//         Func: func(content []byte) []byte {
	//             return patch_v0_13_0(content)
	//         },
	//     },
	// },
}

// Cached verified versions list (loaded on first use)
var versionsVerifiedGenericCompatible []string

const verifiedVersionsURL = "https://raw.githubusercontent.com/lugia19/Claude-WebExtension-Launcher/master/resources/verified_versions.json"

var (
	AppFolder       = utils.ResolvePath(appFolderName)
	appResourcesDir string
	appExePath      string
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

// Load verified versions from GitHub, with fallback to embedded JSON
func loadVerifiedVersions() []string {
	// Try fetching from GitHub first
	resp, err := http.Get(verifiedVersionsURL)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			body, err := io.ReadAll(resp.Body)
			if err == nil {
				var versions []string
				if err := json.Unmarshal(body, &versions); err == nil {
					fmt.Printf("Loaded %d verified versions from GitHub\n", len(versions))
					return versions
				}
			}
		}
	}

	// Fallback to embedded JSON
	fmt.Println("Falling back to embedded verified versions list")
	embeddedData, err := EmbeddedFS.ReadFile("resources/verified_versions.json")
	if err != nil {
		fmt.Printf("Warning: Could not load embedded verified versions: %v\n", err)
		return []string{}
	}

	var versions []string
	if err := json.Unmarshal(embeddedData, &versions); err != nil {
		fmt.Printf("Warning: Could not parse embedded verified versions: %v\n", err)
		return []string{}
	}

	fmt.Printf("Loaded %d verified versions from embedded file\n", len(versions))
	return versions
}

// Check if a version is verified to work with generic patches
func isVersionVerified(version string) bool {
	// Load versions on first use
	if versionsVerifiedGenericCompatible == nil {
		versionsVerifiedGenericCompatible = loadVerifiedVersions()
	}

	for _, v := range versionsVerifiedGenericCompatible {
		if v == version {
			return true
		}
	}
	return false
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

func patch_generic(content []byte) []byte {
	contentStr := string(content)

	// First, find the injection point
	returnPattern := regexp.MustCompile(`return\s*.*?(\w+)\.on\("resize"`)
	matches := returnPattern.FindStringSubmatch(contentStr)
	if matches == nil || len(matches) < 2 {
		fmt.Printf("Warning: Could not find injection point (return.*.on(\"resize\"))\n")
		return content
	}

	// Extract mainWindow variable name from the pattern
	mainWindowVar := matches[1]
	fmt.Printf("Detected mainWindow variable: %s\n", mainWindowVar)

	// Find the webView variable by looking for pattern like: variableName.webContents.on("dom-ready"
	// We need to find this before the injection point
	injectionIndex := returnPattern.FindStringIndex(contentStr)[0]
	contentBeforeInjection := contentStr[:injectionIndex]

	// Look for webView pattern
	webViewPattern := regexp.MustCompile(`(\w+)\.webContents\.on\("dom-ready"`)
	webViewMatches := webViewPattern.FindStringSubmatch(contentBeforeInjection)

	webViewVar := ""
	if webViewMatches != nil && len(webViewMatches) >= 2 {
		webViewVar = webViewMatches[1]
		fmt.Printf("Detected webView variable: %s\n", webViewVar)
	} else {
		fmt.Printf("Warning: Could not detect webView variable, falling back to 'r'\n")
		webViewVar = "r" // Fallback to common default
	}

	// Load all injection files
	injectionFiles := []string{
		"extension_loader.js",
		"alarm_polyfill.js",
		"notification_polyfill.js",
		"tabevents_polyfill.js",
	}

	injection, err := readCombinedInjection("generic", injectionFiles)
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
		return content
	}

	// Replace placeholders in the injection with detected variable names
	injection = strings.ReplaceAll(injection, "PLACEHOLDER_MAINWINDOW", mainWindowVar)
	injection = strings.ReplaceAll(injection, "PLACEHOLDER_WEBVIEW", webViewVar)

	// Insert the injection at the injection point
	loc := returnPattern.FindStringIndex(contentStr)
	if loc != nil {
		contentStr = contentStr[:loc[0]] + "\n" + injection + "\n" + contentStr[loc[0]:]
	}

	// Second injection point - add chrome-extension: to the array
	gxPattern := `["devtools:", "file:"]`
	gxReplacement := `["devtools:", "file:", "chrome-extension:"]`
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

func getLatestVersion() (string, string, error) {
	fmt.Printf("Getting latest version for OS: %s\n", runtime.GOOS)

	if runtime.GOOS == "darwin" {
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
	} else {
		// Windows - parse RELEASES file to find the latest version
		resp, err := http.Get(windowsReleasesURL)
		if err != nil {
			return "", "", fmt.Errorf("fetching Windows releases: %v", err)
		}
		defer resp.Body.Close()

		releasesText, _ := io.ReadAll(resp.Body)

		lines := strings.Split(string(releasesText), "\n")

		// Find the latest version from the RELEASES file
		// The format is typically: SHA1 filename size
		var versions []struct {
			version  string
			url      string
			filename string
		}

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if strings.Contains(line, "AnthropicClaude-") && strings.Contains(line, "-full.nupkg") {
				// Extract version from filename
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					filename := parts[1]
					// Extract version from AnthropicClaude-X.Y.Z-full.nupkg
					versionStart := strings.Index(filename, "AnthropicClaude-") + len("AnthropicClaude-")
					versionEnd := strings.Index(filename, "-full.nupkg")
					if versionStart > 0 && versionEnd > versionStart {
						version := filename[versionStart:versionEnd]
						url := strings.Replace(windowsReleasesURL, "RELEASES", filename, 1)
						versions = append(versions, struct {
							version  string
							url      string
							filename string
						}{version, url, filename})
					}
				}
			}
		}

		if len(versions) == 0 {
			fmt.Printf("ERROR: No valid releases found in RELEASES file\n")
			return "", "", fmt.Errorf("no releases found in Windows RELEASES file")
		}

		// Sort versions to find the latest (newest first)
		sort.Slice(versions, func(i, j int) bool {
			partsI := strings.Split(versions[i].version, ".")
			partsJ := strings.Split(versions[j].version, ".")

			for k := 0; k < len(partsI) && k < len(partsJ); k++ {
				numI, _ := strconv.Atoi(partsI[k])
				numJ, _ := strconv.Atoi(partsJ[k])

				if numI != numJ {
					return numI > numJ
				}
			}

			return len(partsI) > len(partsJ)
		})

		// Use the latest version
		latest := versions[0]
		fmt.Printf("Selected latest version: %s, URL: %s\n", latest.version, latest.url)
		return latest.version, latest.url, nil
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

	// Delete the archive file only if KeepNupkgFiles is false
	if !KeepNupkgFiles {
		os.Remove(newVersionDownloadPath)
	} else {
		fmt.Printf("Keeping archive file: %s\n", newVersionZipName)
	}

	return nil
}

func EnsurePatched(forceUpdate bool) error {
	if err := ensureTools(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Get current version
	currentVersion := ""
	claudeVersionFile := filepath.Join(AppFolder, "claude-version.txt")
	if data, err := os.ReadFile(claudeVersionFile); err == nil {
		currentVersion = strings.TrimSpace(string(data))
		fmt.Printf("Current version: %s\n", currentVersion)
	}

	// Get latest version and download URL
	newestVersion, downloadURL, err := getLatestVersion()
	if err != nil {
		// If we have an existing installation, continue using it
		if currentVersion != "" {
			fmt.Printf("Warning: %v\n", err)
			fmt.Printf("Continuing with existing installation (version %s)\n", currentVersion)

			// Check if the app executable exists
			if _, err := os.Stat(appExePath); os.IsNotExist(err) {
				return fmt.Errorf("existing installation is incomplete (executable not found)")
			}

			return nil // Continue with existing installation
		}
		// No existing installation and no version available
		return fmt.Errorf("no versions available and no existing installation found")
	}

	fmt.Printf("Latest version: %s\n", newestVersion)

	// Check if version is verified
	versionVerified := isVersionVerified(newestVersion)

	// Decide whether to update based on verification status and existing installation
	shouldUpdate := false
	if forceUpdate {
		// Force update regardless of version verification
		shouldUpdate = (currentVersion != newestVersion)
		if shouldUpdate {
			fmt.Printf("Force update enabled, updating to version %s...\n", newestVersion)
			if !versionVerified {
				fmt.Printf("WARNING: Version %s is not verified compatible.\n", newestVersion)
			}
		}
	} else if versionVerified {
		// Always update to verified versions
		shouldUpdate = (currentVersion != newestVersion)
		if shouldUpdate {
			fmt.Printf("Version %s is verified compatible, updating...\n", newestVersion)
		}
	} else {
		// Unverified version and not forcing
		if currentVersion == "" {
			// No existing installation - try the new version
			shouldUpdate = true
			fmt.Printf("WARNING: Version %s is not verified compatible, but no existing installation found.\n", newestVersion)
			fmt.Println("Attempting installation anyway...")
		} else if currentVersion == newestVersion {
			// Already on this unverified version
			shouldUpdate = false
			fmt.Printf("Already on version %s (unverified but currently installed)\n", newestVersion)
		} else {
			// Have existing installation and new version is unverified - stay on current
			shouldUpdate = false
			fmt.Printf("WARNING: Version %s is not verified compatible.\n", newestVersion)
			fmt.Printf("Keeping existing installation (version %s) to avoid potential issues.\n", currentVersion)
			fmt.Println("To force update, use --force-update flag.")
		}
	}

	// Update if needed
	if shouldUpdate {
		fmt.Printf("Updating to %s...\n", newestVersion)

		if err := downloadAndExtract(newestVersion, downloadURL); err != nil {
			return err
		}

		// Write version
		os.WriteFile(claudeVersionFile, []byte(newestVersion), 0644)

		// Apply patches
		if err := applyPatches(newestVersion); err != nil {
			return fmt.Errorf("applying patches: %v", err)
		}
	} else if currentVersion == newestVersion {
		fmt.Println("Already on the latest version")
	}

	return nil
}

// Helper function to apply a patch to a single file
func applyPatchToFile(filePath string, content []byte, patchFunc func([]byte) []byte) bool {
	// Get the base filename for logging
	fileName := filepath.Base(filePath)

	// Beautify JS files if needed
	if strings.HasSuffix(fileName, ".js") {
		if !bytes.Contains(content, []byte("/* CLAUDE-MANAGER-BEAUTIFIED */")) {
			// Beautify the file
			var beautifyCmd *exec.Cmd
			if runtime.GOOS == "windows" && strings.HasSuffix(jsBeautifyCmd, ".ps1") {
				beautifyCmd = exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", jsBeautifyCmd, filePath, "-o", filePath)
			} else {
				beautifyCmd = exec.Command(jsBeautifyCmd, filePath, "-o", filePath)
			}
			if err := beautifyCmd.Run(); err != nil {
				fmt.Printf("Warning: Could not beautify %s: %v\n", fileName, err)
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

	// Apply the patch
	newContent := patchFunc(content)
	if err := os.WriteFile(filePath, newContent, 0644); err != nil {
		fmt.Printf("Failed to write %s: %v\n", fileName, err)
		return false
	}

	return true
}

func applyPatches(version string) error {
	// Try version-specific patches first, fall back to generic
	patches, ok := supportedVersions[version]
	if !ok || len(patches) == 0 {
		// Try generic patches
		patches, ok = supportedVersions["generic"]
		if !ok || len(patches) == 0 {
			fmt.Printf("No patches available for version %s (and no generic patches found)\n", version)
			return nil
		}
		fmt.Printf("Using generic patches for version %s\n", version)
	} else {
		fmt.Printf("Using version-specific patches for %s\n", version)
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

		// Try each file pattern for this patch
		patchApplied := false
		for _, filePattern := range patch.Files {
			// Check if this is a pattern (contains *) or exact filename
			if strings.Contains(filePattern, "*") {
				// Use filepath.Glob to find matching files
				pattern := filepath.Join(tempDir, filePattern)
				matches, err := filepath.Glob(pattern)
				if err != nil {
					fmt.Printf("Error with pattern %s: %v\n", filePattern, err)
					continue
				}

				fmt.Printf("Pattern %s matched %d files\n", filePattern, len(matches))

				// Apply patch to all matching files
				for _, matchedFile := range matches {
					relPath, _ := filepath.Rel(tempDir, matchedFile)
					fmt.Printf("Trying matched file: %s\n", relPath)

					content, err := os.ReadFile(matchedFile)
					if err != nil {
						fmt.Printf("  Skipping %s: %v\n", relPath, err)
						continue
					}

					if applyPatchToFile(matchedFile, content, patch.Func) {
						fmt.Printf("  Successfully applied patch to %s\n", relPath)
						patchApplied = true
					}
				}
			} else {
				// Exact filename
				filePath := filepath.Join(tempDir, filePattern)
				content, err := os.ReadFile(filePath)
				if err != nil {
					fmt.Printf("Skipping %s: %v\n", filePattern, err)
					continue
				}

				fmt.Printf("Found file: %s\n", filePattern)
				if applyPatchToFile(filePath, content, patch.Func) {
					fmt.Printf("Successfully applied patch to %s\n", filePattern)
					patchApplied = true
					break // For exact files, one success is enough
				}
			}

			if patchApplied {
				break // Move to next patch
			}
		}

		if !patchApplied {
			return fmt.Errorf("patch %d failed: none of the target files could be patched", i+1)
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

	// Ad-hoc sign on macOS after all modifications
	if runtime.GOOS == "darwin" {
		fmt.Println("Signing app with ad-hoc signature...")
		appPath := filepath.Join(AppFolder, "Claude.app")

		// Remove existing signature first
		cmd := exec.Command("codesign", "--remove-signature", appPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Remove signature output: %s\n", string(output))
			// Ignore errors, might not be signed
		} else if len(output) > 0 {
			fmt.Printf("Remove signature output: %s\n", string(output))
		}

		// Sign with ad-hoc signature
		cmd = exec.Command("codesign", "--force", "--deep", "--sign", "-", appPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Warning: Could not sign app: %v\n%s\n", err, string(output))
			// Continue anyway - might still work
		} else {
			fmt.Printf("App signed successfully\n")
			if len(output) > 0 {
				fmt.Printf("Signing output: %s\n", string(output))
			}
		}
	}

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
	if runtime.GOOS == "darwin" {
		fmt.Println("Signing app with ad-hoc signature...")
		appPath := filepath.Join(AppFolder, "Claude.app")

		// Remove existing signature first
		cmd := exec.Command("codesign", "--remove-signature", appPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Remove signature output: %s\n", string(output))
			// Ignore errors, might not be signed
		} else if len(output) > 0 {
			fmt.Printf("Remove signature output: %s\n", string(output))
		}

		// Sign with ad-hoc signature
		cmd = exec.Command("codesign", "--force", "--deep", "--sign", "-", appPath)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Warning: Could not sign app: %v\n%s\n", err, string(output))
			// Continue anyway - might still work
		} else {
			fmt.Printf("App signed successfully\n")
			if len(output) > 0 {
				fmt.Printf("Signing output: %s\n", string(output))
			}
		}
	}

	fmt.Println("Patches applied successfully!")
	return nil
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
	if runtime.GOOS == "darwin" {
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
