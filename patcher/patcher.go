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
	"0.12.55": {
		{
			Files: []string{".vite/build/index-BZRfNpEg.js", ".vite/build/index-DyHP6ri_.js"},
			Func: func(content []byte) []byte {
				return patch_generic(content, "0.12.55")
			},
		},
	},
	"0.12.112": {
		{
			Files: []string{".vite/build/index.js"},
			Func: func(content []byte) []byte {
				return patch_generic(content, "0.12.55")
			},
		},
	},
}

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

func patch_generic(content []byte, version string) []byte {
	contentStr := string(content)

	// Load all injection files
	injectionFiles := []string{
		"extension_loader.js",
		"alarm_polyfill.js",
		"notification_polyfill.js",
		"tabevents_polyfill.js",
	}

	injection, err := readCombinedInjection(version, injectionFiles)
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
		return content
	}

	// First injection point - using regex to match return.*.on("resize"
	returnPattern := regexp.MustCompile(`return.*\.on\("resize`)
	if loc := returnPattern.FindStringIndex(contentStr); loc != nil {
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

func getLatestSupportedVersion() (string, string, error) {
	// Get supported supportedVersionsList sorted newest first
	supportedVersionsList := make([]string, 0, len(supportedVersions))
	for v := range supportedVersions {
		supportedVersionsList = append(supportedVersionsList, v)
	}

	// Sort with proper version comparison (newest first)
	sort.Slice(supportedVersionsList, func(i, j int) bool {
		partsI := strings.Split(supportedVersionsList[i], ".")
		partsJ := strings.Split(supportedVersionsList[j], ".")

		for k := 0; k < len(partsI) && k < len(partsJ); k++ {
			numI, _ := strconv.Atoi(partsI[k])
			numJ, _ := strconv.Atoi(partsJ[k])

			if numI != numJ {
				return numI > numJ // > for descending order (newest first)
			}
		}

		// If all compared parts are equal, the one with more parts is newer
		// e.g., "0.12.55.1" > "0.12.55"
		return len(partsI) > len(partsJ)
	})

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
		fmt.Println("Removed temporary archive file")
	} else {
		fmt.Printf("Keeping archive file: %s\n", newVersionZipName)
	}

	return nil
}

func EnsurePatched() error {
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

	// Get newest supported version and download URL
	newestVersion, downloadURL, err := getLatestSupportedVersion()
	if err != nil {
		return err
	}

	fmt.Printf("Newest supported version: %s\n", newestVersion)

	// Update if needed
	if currentVersion != newestVersion {
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
