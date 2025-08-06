package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	releasesURL = "https://storage.googleapis.com/osprey-downloads-c02f6a0d-347c-492b-a752-3e0651722e97/nest-win-x64/RELEASES"
	appFolder   = "app-latest"
)

type Patch struct {
	File string
	Func func(content []byte) []byte
}

var supportedVersions = map[string][]Patch{
	"0.12.55": {
		{
			File: ".vite/build/index-BZRfNpEg.js",
			Func: patch_0_12_55,
		},
	},
}

// Patch functions
func readInjection(version, filename string) (string, error) {
	injectionPath := filepath.Join("resources", "injections", version, filename)
	content, err := os.ReadFile(injectionPath)
	if err != nil {
		return "", fmt.Errorf("could not load injection %s for version %s: %v", filename, version, err)
	}
	return string(content), nil
}

func patch_0_12_55(content []byte) []byte {
	contentStr := string(content)

	// Load injection from file
	injection, err := readInjection("0.12.55", "extension-loader.js")
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
		return content
	}

	// First injection point - before return statement inside use();
	returnPattern := `return a(), i(), e.on("resize", () => {`
	if idx := strings.Index(contentStr, returnPattern); idx != -1 {
		// Insert right before 'return'
		contentStr = contentStr[:idx] + "\n" + injection + "\n" + contentStr[idx:]
	}

	// Second injection point - add chrome-extension: to the array
	gxPattern := `gX = ["devtools:", "file:"]`
	gxReplacement := `gX = ["devtools:", "file:", "chrome-extension:"]`
	contentStr = strings.Replace(contentStr, gxPattern, gxReplacement, 1)

	return []byte(contentStr)
}

type Extension struct {
	Owner  string // GitHub owner
	Repo   string // GitHub repo name
	Folder string // Local folder name in extensions/
}

var extensions = []Extension{
	{Owner: "lugia19", Repo: "Claude-Usage-Extension", Folder: "usage-tracker"},
	// Add more as needed
}

var (
	asarCmd       string
	jsBeautifyCmd string
)

func getCurrentVersion() string {
	data, err := os.ReadFile("version.txt")
	if err != nil {
		return "0.0.0" // If no version file, assume ancient
	}
	return strings.TrimSpace(string(data))
}

func finishUpdateIfNeeded() {
	exePath, _ := os.Executable()
	exeName := filepath.Base(exePath)

	// If we're running as .new.exe, replace the original
	if strings.HasSuffix(exeName, ".new.exe") {
		originalExe := strings.TrimSuffix(exePath, ".new.exe") + ".exe"

		// Wait a bit for the original to fully exit
		time.Sleep(500 * time.Millisecond)

		// Try to delete with retries
		for i := 0; i < 5; i++ {
			if err := os.Remove(originalExe); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Copy ourselves to the original name
		input, _ := os.ReadFile(exePath)
		if err := os.WriteFile(originalExe, input, 0755); err != nil {
			fmt.Printf("Failed to write update: %v\n", err)
			os.Exit(1)
		}

		// Launch the original
		cmd := exec.Command(originalExe)
		cmd.Start()

		os.Exit(0)
	}

	// If we're the main exe, clean up any .new.exe
	newExePath := strings.TrimSuffix(exePath, ".exe") + ".new.exe"
	os.Remove(newExePath) // Clean up if it exists
}

func selfUpdate() error {
	fmt.Println("Checking for installer updates...")

	currentVersion := getCurrentVersion()

	// Check latest release
	url := "https://api.github.com/repos/lugia19/claude-webext-patcher/releases/latest"
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name        string `json:"name"`
			DownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return err
	}

	// Strip 'v' prefix if present
	latestVersion := strings.TrimPrefix(release.TagName, "v")

	if currentVersion == latestVersion {
		fmt.Println("Installer is up to date")
		return nil
	}

	fmt.Printf("Update available: %s -> %s\n", currentVersion, latestVersion)

	// Find the zip asset
	var downloadURL string
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, ".zip") {
			downloadURL = asset.DownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no zip file found in release")
	}

	// Download to temp
	fmt.Println("Downloading update...")
	tempZip := "update-temp.zip"
	resp, err = http.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, _ := os.Create(tempZip)
	io.Copy(out, resp.Body)
	out.Close()
	defer os.Remove(tempZip)

	// Extract to temp dir
	fmt.Println("Extracting update...")
	tempDir := "update-temp"
	os.RemoveAll(tempDir)
	os.MkdirAll(tempDir, 0755)
	defer os.RemoveAll(tempDir)

	// Extract zip
	zipReader, _ := zip.OpenReader(tempZip)
	for _, f := range zipReader.File {
		path := filepath.Join(tempDir, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			continue
		}

		os.MkdirAll(filepath.Dir(path), 0755)

		src, _ := f.Open()
		dst, _ := os.Create(path)
		io.Copy(dst, src)
		dst.Close()
		src.Close()
	}
	zipReader.Close()

	fmt.Println("Installing update...")

	// Replace resources folder
	os.RemoveAll("resources")
	os.Rename(filepath.Join(tempDir, "resources"), "resources")

	// Update version.txt
	versionData, _ := os.ReadFile(filepath.Join(tempDir, "version.txt"))
	os.WriteFile("version.txt", versionData, 0644)

	// Copy new exe as .new.exe
	exePath, _ := os.Executable()
	newExeName := strings.TrimSuffix(filepath.Base(exePath), ".exe") + ".new.exe"

	newExeData, _ := os.ReadFile(filepath.Join(tempDir, "claude-webext-patcher.exe"))
	os.WriteFile(newExeName, newExeData, 0755)

	fmt.Println("Restarting to complete update...")

	// Launch the new exe
	cmd := exec.Command(newExeName)
	cmd.Start()

	// Exit to let it take over
	os.Exit(0)

	return nil
}

func main() {
	// Handle update completion first
	finishUpdateIfNeeded()

	fmt.Println("Claude Manager starting...")

	// Check for self-updates
	if err := selfUpdate(); err != nil {
		fmt.Printf("Update check failed: %v\n", err)
		// Continue anyway
	}

	// Ensure we have asar available
	if err := ensureTools(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Ensure app is installed and updated
	if err := ensureUpdatedApp(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Update extensions
	if err := updateExtensions(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Launch Claude
	fmt.Println("Launching Claude. If it appears blank, press CTRL+R a couple times, or restart the application.")
	cmd := exec.Command(filepath.Join(appFolder, "claude.exe"))
	cmd.Start()
}

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
	nodeModulesPath := filepath.Join(".", "node_modules", ".bin")
	asarCmd = filepath.Join(nodeModulesPath, "asar")
	jsBeautifyCmd = filepath.Join(nodeModulesPath, "js-beautify")

	if runtime.GOOS == "windows" {
		asarCmd += ".cmd"
		jsBeautifyCmd += ".cmd"
	}

	// Install locally if needed
	if _, err := os.Stat(asarCmd); os.IsNotExist(err) {
		fmt.Println("Installing tools locally...")

		// Choose asar package based on Node version
		asarPackage := "asar"
		if majorVersion >= 22 {
			asarPackage = "@electron/asar"
		}

		cmd := exec.Command("npm", "install", "--no-save", asarPackage, "js-beautify")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install tools: %v", err)
		}
	}

	return nil
}

func ensureUpdatedApp() error {
	// Get current version
	currentVersion := ""
	versionFile := filepath.Join(appFolder, "version.txt")
	if data, err := os.ReadFile(versionFile); err == nil {
		currentVersion = strings.TrimSpace(string(data))
		fmt.Printf("Current version: %s\n", currentVersion)
	}

	// Get available releases
	resp, err := http.Get(releasesURL)
	if err != nil {
		return fmt.Errorf("fetching releases: %v", err)
	}
	defer resp.Body.Close()

	releasesText, _ := io.ReadAll(resp.Body)

	// Find newest supported version
	versions := make([]string, 0, len(supportedVersions))
	for v := range supportedVersions {
		versions = append(versions, v)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(versions)))

	newestVersion := ""
	for _, version := range versions {
		filename := fmt.Sprintf("AnthropicClaude-%s-full.nupkg", version)
		if strings.Contains(string(releasesText), filename) {
			newestVersion = version
			break
		}
	}

	if newestVersion == "" {
		return fmt.Errorf("no supported versions available")
	}

	fmt.Printf("Newest supported version: %s\n", newestVersion)

	// Update if needed
	if currentVersion != newestVersion {
		fmt.Printf("Updating to %s...\n", newestVersion)

		// Download
		filename := fmt.Sprintf("AnthropicClaude-%s-full.nupkg", newestVersion)
		downloadURL := strings.Replace(releasesURL, "RELEASES", filename, 1)

		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("downloading: %v", err)
		}
		defer resp.Body.Close()

		tempFile, _ := os.Create(filename + ".tmp")
		io.Copy(tempFile, resp.Body)
		tempFile.Close()

		// Extract
		fmt.Println("Extracting...")
		os.RemoveAll(appFolder)
		os.MkdirAll(appFolder, 0755)

		zipReader, _ := zip.OpenReader(filename + ".tmp")
		for _, f := range zipReader.File {
			// Only extract files from lib/net45/
			if !strings.HasPrefix(f.Name, "lib/net45/") {
				continue
			}

			// Strip the lib/net45/ prefix when extracting
			relativePath := strings.TrimPrefix(f.Name, "lib/net45/")
			if relativePath == "" {
				continue
			}

			path := filepath.Join(appFolder, relativePath)

			if f.FileInfo().IsDir() {
				os.MkdirAll(path, f.Mode())
				continue
			}

			os.MkdirAll(filepath.Dir(path), 0755)

			src, _ := f.Open()
			dst, _ := os.Create(path)
			io.Copy(dst, src)
			dst.Close()
			src.Close()
		}
		zipReader.Close()
		os.Remove(filename + ".tmp")

		// Write version
		os.WriteFile(versionFile, []byte(newestVersion), 0644)

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

	asarPath := filepath.Join(appFolder, "resources", "app.asar")
	tempDir := "asar-temp"

	// Unpack asar
	fmt.Println("Unpacking asar...")
	cmd := exec.Command(asarCmd, "extract", asarPath, tempDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unpacking asar: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Apply patches
	for _, patch := range patches {
		filePath := filepath.Join(tempDir, patch.File)
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("reading %s: %v", patch.File, err)
		}

		if strings.HasSuffix(patch.File, ".js") {
			// Check if already beautified
			if !bytes.Contains(content, []byte("/* CLAUDE-MANAGER-BEAUTIFIED */")) {
				// Beautify the file
				cmd := exec.Command(jsBeautifyCmd, filePath, "-o", filePath)
				if err := cmd.Run(); err != nil {
					fmt.Printf("Warning: Could not beautify %s: %v\n", patch.File, err)
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
		fmt.Printf("Patching %s (first %d chars): %s...\n", patch.File, debugLen, string(content[:debugLen]))

		newContent := patch.Func(content)
		if err := os.WriteFile(filePath, newContent, 0644); err != nil {
			return fmt.Errorf("writing %s: %v", patch.File, err)
		}
	}

	// Backup original
	os.Rename(asarPath, asarPath+".backup")

	// Repack asar
	fmt.Println("Repacking asar...")
	cmd = exec.Command(asarCmd, "pack", tempDir, asarPath)
	if err := cmd.Run(); err != nil {
		os.Rename(asarPath+".backup", asarPath) // Restore on failure
		return fmt.Errorf("repacking asar: %v", err)
	}

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

	fmt.Println("Patches applied successfully!")
	return nil
}

func replaceIcons() error {
	iconDir := filepath.Join("resources", "icons")
	if _, err := os.Stat(iconDir); os.IsNotExist(err) {
		return fmt.Errorf("icons folder not found")
	}

	fmt.Println("Replacing icons...")

	// OS-specific exe icon replacement
	switch runtime.GOOS {
	case "windows":
		rceditPath := filepath.Join("resources", "rcedit.exe")
		icoPath := filepath.Join(iconDir, "app.ico")
		exePath := filepath.Join(appFolder, "claude.exe")

		cmd := exec.Command(rceditPath, exePath, "--set-icon", icoPath)
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: Could not replace exe icon: %v\n", err)
		}
	case "darwin":
		// macOS: Would need to replace .icns in the .app bundle
		// TODO when adding macOS support
	case "linux":
		// Linux: Icons are typically just .desktop files
		// TODO when adding Linux support
	}

	// Copy PNG icons (works for all platforms)
	entries, err := os.ReadDir(iconDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".png") {
			continue
		}

		src := filepath.Join(iconDir, entry.Name())
		dst := filepath.Join(appFolder, "resources", entry.Name())

		fmt.Printf("  %s -> %s\n", entry.Name(), dst)

		input, err := os.ReadFile(src)
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
	cmd := exec.Command(filepath.Join(appFolder, "claude.exe"))
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

func replaceHashInExe(oldHash, newHash string) error {
	exePath := filepath.Join(appFolder, "claude.exe")

	// Read the entire exe
	data, err := os.ReadFile(exePath)
	if err != nil {
		return err
	}

	// Search for the hash as a string
	replaced := bytes.Replace(data, []byte(oldHash), []byte(newHash), 1)
	if bytes.Equal(replaced, data) {
		return fmt.Errorf("hash not found in exe")
	}

	// Write back
	return os.WriteFile(exePath, replaced, 0755)
}

func updateExtensions() error {
	fmt.Println("Checking extensions...")

	// Create extensions dir if needed
	os.MkdirAll("extensions", 0755)

	for _, ext := range extensions {
		// Check current version
		manifestPath := filepath.Join("extensions", ext.Folder, "manifest.json")
		currentVersion := ""

		if data, err := os.ReadFile(manifestPath); err == nil {
			var manifest struct {
				Version string `json:"version"`
			}
			if json.Unmarshal(data, &manifest) == nil {
				currentVersion = manifest.Version
			}
		}

		// Get latest release from GitHub
		url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", ext.Owner, ext.Repo)
		resp, err := http.Get(url)
		if err != nil {
			fmt.Printf("  %s: error checking: %v\n", ext.Folder, err)
			continue
		}

		var release struct {
			TagName string `json:"tag_name"`
			Assets  []struct {
				Name        string `json:"name"`
				DownloadURL string `json:"browser_download_url"`
			} `json:"assets"`
		}

		json.NewDecoder(resp.Body).Decode(&release)
		resp.Body.Close()

		// Check if update needed
		releaseVersion := strings.TrimPrefix(release.TagName, "v")

		// Check if update needed
		if currentVersion == releaseVersion {
			fmt.Printf("  %s: up to date (%s)\n", ext.Folder, currentVersion)
			continue
		}

		// Find electron zip
		downloadURL := ""
		for _, asset := range release.Assets {
			if strings.Contains(strings.ToLower(asset.Name), "electron") && strings.HasSuffix(asset.Name, ".zip") {
				downloadURL = asset.DownloadURL
				break
			}
		}

		if downloadURL == "" {
			fmt.Printf("  %s: no electron zip found\n", ext.Folder)
			continue
		}

		fmt.Printf("  %s: updating %s -> %s\n", ext.Folder, currentVersion, release.TagName)

		// Download and extract
		if err := downloadAndExtractExtension(downloadURL, ext.Folder); err != nil {
			fmt.Printf("  %s: error updating: %v\n", ext.Folder, err)
		}
	}

	return nil
}

func downloadAndExtractExtension(url, folder string) error {
	// Download to temp
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	tempFile := folder + "-temp.zip"
	out, _ := os.Create(tempFile)
	io.Copy(out, resp.Body)
	out.Close()
	defer os.Remove(tempFile)

	// Remove old and extract new
	extPath := filepath.Join("extensions", folder)
	os.RemoveAll(extPath)
	os.MkdirAll(extPath, 0755)

	// Extract zip
	zipReader, err := zip.OpenReader(tempFile)
	if err != nil {
		return err
	}
	defer zipReader.Close()

	for _, f := range zipReader.File {
		path := filepath.Join(extPath, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
			continue
		}

		os.MkdirAll(filepath.Dir(path), 0755)

		src, _ := f.Open()
		dst, _ := os.Create(path)
		io.Copy(dst, src)
		dst.Close()
		src.Close()
	}

	return nil
}
