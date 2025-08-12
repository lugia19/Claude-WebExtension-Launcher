package patcher

import (
	"archive/zip"
	"bytes"
	"claude-webext-patcher/utils"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	releasesURL    = "https://storage.googleapis.com/osprey-downloads-c02f6a0d-347c-492b-a752-3e0651722e97/nest-win-x64/RELEASES"
	appFolderName  = "app-latest"
	KeepNupkgFiles = true
)

var AppFolder = utils.ResolvePath(appFolderName)

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

// Patch functions
func readInjection(version, filename string) (string, error) {
	injectionPath := utils.ResolvePath(filepath.Join("resources", "injections", version, filename))
	content, err := os.ReadFile(injectionPath)
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

func EnsurePatched() error {
	if err := ensureTools(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// Get current version
	currentVersion := ""
	versionFile := filepath.Join(AppFolder, "version.txt")
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

		filename := fmt.Sprintf("AnthropicClaude-%s-full.nupkg", newestVersion)

		// Check if file already exists when KeepNupkgFiles is enabled
		fileExists := false
		fullPath := utils.ResolvePath(filename)
		if _, err := os.Stat(fullPath); err == nil {
			fileExists = true
		}

		if KeepNupkgFiles && fileExists {
			fmt.Printf("Using existing nupkg: %s\n", filename)
		} else {
			// Download if file doesn't exist or if we're not keeping files
			downloadURL := strings.Replace(releasesURL, "RELEASES", filename, 1)
			fmt.Printf("Downloading from: %s\n", downloadURL)

			resp, err := http.Get(downloadURL)
			if err != nil {
				return fmt.Errorf("downloading: %v", err)
			}
			defer resp.Body.Close()

			// Use temp file if we're not keeping the nupkg
			targetFile := utils.ResolvePath(filename)
			if !KeepNupkgFiles {
				targetFile = utils.ResolvePath(filename + ".tmp")
			}

			outFile, err := os.Create(targetFile)
			if err != nil {
				return fmt.Errorf("creating file: %v", err)
			}
			_, err = io.Copy(outFile, resp.Body)
			outFile.Close()
			if err != nil {
				return fmt.Errorf("saving file: %v", err)
			}
			fmt.Printf("Downloaded: %s\n", targetFile)

			// If using temp file, rename for extraction
			if !KeepNupkgFiles {
				filename = targetFile
			}
		}

		// Extract
		fmt.Println("Extracting...")
		os.RemoveAll(AppFolder)
		os.MkdirAll(AppFolder, 0755)

		filePath := filename
		if !KeepNupkgFiles {
			filePath = filename + ".tmp"
		}
		zipReader, err := zip.OpenReader(utils.ResolvePath(filePath))
		if err != nil {
			return fmt.Errorf("opening nupkg: %v", err)
		}
		defer zipReader.Close()

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

			path := filepath.Join(AppFolder, relativePath)

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

		// Delete the nupkg file only if KeepNupkgFiles is false
		if !KeepNupkgFiles {
			os.Remove(utils.ResolvePath(filePath))
			fmt.Println("Removed temporary nupkg file")
		} else {
			fmt.Printf("Keeping nupkg file: %s\n", filename)
		}

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

	asarPath := filepath.Join(AppFolder, "resources", "app.asar")
	tempDir := utils.ResolvePath("asar-temp")

	// Unpack asar
	fmt.Println("Unpacking asar...")
	cmd := exec.Command(asarCmd, "extract", asarPath, tempDir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unpacking asar: %v", err)
	}
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
					cmd := exec.Command(jsBeautifyCmd, filePath, "-o", filePath)
					if err := cmd.Run(); err != nil {
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
	iconDir := utils.ResolvePath(filepath.Join("resources", "icons"))
	if _, err := os.Stat(iconDir); os.IsNotExist(err) {
		return fmt.Errorf("icons folder not found")
	}

	fmt.Println("Replacing icons...")

	// OS-specific exe icon replacement
	switch runtime.GOOS {
	case "windows":
		rceditPath := utils.ResolvePath(filepath.Join("resources", "rcedit.exe"))
		icoPath := filepath.Join(iconDir, "app.ico")
		exePath := filepath.Join(AppFolder, "claude.exe")

		cmd := exec.Command(rceditPath, exePath, "--set-icon", icoPath)
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: Could not replace exe icon: %v\n", err)
		}
	case "darwin":
		// macOS: Would need to replace .icns in the .app bundle
		// TODO when adding macOS support
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
		dst := filepath.Join(AppFolder, "resources", entry.Name())

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
	cmd := exec.Command(filepath.Join(AppFolder, "claude.exe"))
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
	exePath := filepath.Join(AppFolder, "claude.exe")

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
