package selfupdate

import (
	"archive/zip"
	"claude-webext-patcher/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func currentVersion() string {
	data, err := os.ReadFile(utils.ResolvePath("version.txt"))
	if err != nil {
		return "0.0.0" // If no version file, assume ancient
	}
	return strings.TrimSpace(string(data))
}

// getPlatformSuffix returns the expected platform suffix for release files
func getPlatformSuffix() string {
	switch runtime.GOOS {
	case "windows":
		return "-windows"
	case "darwin":
		return "-macos"
	default:
		return "-" + runtime.GOOS
	}
}

// getExecutableName returns the expected executable name for the current platform
func getExecutableName() string {
	execName := "Claude_WebExtension_Launcher"
	if runtime.GOOS == "windows" {
		execName += ".exe"
	}
	return execName
}

func FinishUpdateIfNeeded() {
	exePath, _ := os.Executable()
	exeName := filepath.Base(exePath)

	// Platform-specific handling for Windows
	if runtime.GOOS == "windows" && strings.HasSuffix(exeName, ".new.exe") {
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

		// Launch the original in new console window
		cmd := exec.Command("cmd", "/c", "start", originalExe)
		cmd.Start()

		os.Exit(0)
	}

	// For Unix-like systems, check for .new suffix
	if runtime.GOOS != "windows" && strings.HasSuffix(exeName, ".new") {
		originalExe := strings.TrimSuffix(exePath, ".new")

		// Wait for original to exit
		time.Sleep(500 * time.Millisecond)

		// Try to replace with retries
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

		// Launch the original - on macOS, make sure it's in Terminal
		if runtime.GOOS == "darwin" {
			execDir := filepath.Dir(originalExe)
			script := fmt.Sprintf(`tell application "Terminal"
				do script "cd '%s' && '%s' && exit"
				activate
			end tell`, execDir, originalExe)

			cmd := exec.Command("osascript", "-e", script)
			cmd.Start()
		} else {
			cmd := exec.Command(originalExe)
			cmd.Start()
		}

		os.Exit(0)
	}

	// Clean up any temporary update files
	if runtime.GOOS == "windows" {
		newExePath := strings.TrimSuffix(exePath, ".exe") + ".new.exe"
		os.Remove(newExePath)
	} else {
		newExePath := exePath + ".new"
		os.Remove(newExePath)
	}
}

func CheckAndUpdate() error {
	fmt.Printf("Checking for installer updates on %s...\n", runtime.GOOS)

	currentVer := currentVersion()
	platformSuffix := getPlatformSuffix()

	// Check latest release
	url := "https://api.github.com/repos/lugia19/claude-webext-patcher/releases/latest"
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %v", err)
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
		return fmt.Errorf("failed to parse release info: %v", err)
	}

	// Strip 'v' prefix if present
	latestVersion := strings.TrimPrefix(release.TagName, "v")

	// Check if we got a valid version
	if latestVersion == "" {
		return fmt.Errorf("failed to get latest version from GitHub")
	}

	if compareVersions(currentVer, latestVersion) >= 0 {
		fmt.Println("Installer is up to date")
		return nil
	}

	fmt.Printf("Update available: %s -> %s\n", currentVer, latestVersion)

	// Find the platform-specific zip asset
	var downloadURL string
	var assetName string

	// First try to find exact platform match
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, platformSuffix) && strings.HasSuffix(asset.Name, ".zip") {
			downloadURL = asset.DownloadURL
			assetName = asset.Name
			break
		}
	}

	// If no platform-specific file found, show available options
	if downloadURL == "" {
		fmt.Printf("No release found for platform: %s\n", runtime.GOOS)
		fmt.Println("Available releases:")
		for _, asset := range release.Assets {
			if strings.HasSuffix(asset.Name, ".zip") {
				fmt.Printf("  - %s\n", asset.Name)
			}
		}
		return fmt.Errorf("no compatible release file found for %s", runtime.GOOS)
	}

	fmt.Printf("Found platform release: %s\n", assetName)

	// Download to temp
	fmt.Println("Downloading update...")
	tempZip := utils.ResolvePath("update-temp.zip")
	resp, err = http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download update: %v", err)
	}
	defer resp.Body.Close()

	out, err := os.Create(tempZip)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tempZip)
		return fmt.Errorf("failed to save update: %v", err)
	}

	// Extract to temp dir
	fmt.Println("Extracting update...")
	tempDir := utils.ResolvePath("update-temp")
	os.RemoveAll(tempDir)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		os.Remove(tempZip)
		return fmt.Errorf("failed to create temp dir: %v", err)
	}

	// Extract zip
	zipReader, err := zip.OpenReader(tempZip)
	if err != nil {
		os.Remove(tempZip)
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to open zip: %v", err)
	}

	for _, f := range zipReader.File {
		// Normalize path separators - replace backslashes with forward slashes
		normalizedName := strings.ReplaceAll(f.Name, "\\", "/")
		// Then use filepath.Join which will use the correct separator for the OS
		path := filepath.Join(tempDir, filepath.FromSlash(normalizedName))

		//fmt.Printf("Processing: %s -> %s (IsDir: %v, Size: %d)\n", f.Name, path, f.FileInfo().IsDir(), f.UncompressedSize64)

		// Treat as directory if IsDir() returns true OR if it's a zero-byte entry ending with slash/backslash
		isDirectory := f.FileInfo().IsDir() || (f.UncompressedSize64 == 0 && (strings.HasSuffix(normalizedName, "/") || strings.HasSuffix(f.Name, "\\")))

		if isDirectory {
			fmt.Printf("Creating directory: %s\n", path)
			os.MkdirAll(path, f.Mode())
			continue
		}

		// Skip if path already exists as a directory (created by earlier MkdirAll)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			fmt.Printf("Skipping %s - already exists as directory\n", path)
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
	os.RemoveAll(utils.ResolvePath("resources"))

	// Platform-specific extraction
	var newExeData []byte
	if runtime.GOOS == "darwin" {
		// On macOS, extract from the .app bundle structure
		appName := "Claude_WebExtension_Launcher.app"

		// Extract resources from the app bundle (they're next to the executable in MacOS/)
		bundleResourcesPath := filepath.Join(tempDir, appName, "Contents", "MacOS")
		if info, err := os.Stat(bundleResourcesPath); err == nil && info.IsDir() {
			// Copy resources folder if it exists
			resourcesSrc := filepath.Join(bundleResourcesPath, "resources")
			if _, err := os.Stat(resourcesSrc); err == nil {
				if err := os.Rename(resourcesSrc, utils.ResolvePath("resources")); err != nil {
					fmt.Printf("Note: Could not update resources folder: %v\n", err)
				}
			}
		}

		// Extract the actual executable from the app bundle
		bundleExePath := filepath.Join(tempDir, appName, "Contents", "MacOS", "Claude_WebExtension_Launcher")
		newExeData, err = os.ReadFile(bundleExePath)
		if err != nil {
			os.Remove(tempZip)
			os.RemoveAll(tempDir)
			return fmt.Errorf("failed to find executable in app bundle: %v", err)
		}

		// Look for version.txt next to the executable in the app bundle
		versionPath := filepath.Join(tempDir, appName, "Contents", "MacOS", "version.txt")
		if versionData, err := os.ReadFile(versionPath); err == nil {
			os.WriteFile(utils.ResolvePath("version.txt"), versionData, 0644)
		} else {
			fmt.Printf("Note: No version.txt found in update\n")
		}

	} else {
		// Windows - flat structure
		if err := os.Rename(filepath.Join(tempDir, "resources"), utils.ResolvePath("resources")); err != nil {
			fmt.Printf("Note: No resources folder in update\n")
		}

		// Update version.txt
		versionData, err := os.ReadFile(filepath.Join(tempDir, "version.txt"))
		if err == nil {
			os.WriteFile(utils.ResolvePath("version.txt"), versionData, 0644)
		}

		// Look for the executable in the temp dir
		executableName := getExecutableName() // This returns claude-webext-patcher.exe on Windows
		newExeData, err = os.ReadFile(filepath.Join(tempDir, executableName))
		if err != nil {
			os.Remove(tempZip)
			os.RemoveAll(tempDir)
			return fmt.Errorf("failed to find executable in update: %v", err)
		}
	}

	// Now write the new executable with platform-specific naming
	exePath, _ := os.Executable()
	var newExeName string
	if runtime.GOOS == "windows" {
		newExeName = utils.ResolvePath(strings.TrimSuffix(filepath.Base(exePath), ".exe") + ".new.exe")
	} else {
		newExeName = utils.ResolvePath(filepath.Base(exePath) + ".new")
	}

	if err := os.WriteFile(newExeName, newExeData, 0755); err != nil {
		os.Remove(tempZip)
		os.RemoveAll(tempDir)
		return fmt.Errorf("failed to write new executable: %v", err)
	}

	// Clean up temp files before restarting
	os.Remove(tempZip)
	os.RemoveAll(tempDir)

	fmt.Println("Restarting to complete update...")

	// Launch the new exe
	cmd := exec.Command(newExeName)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start updated executable: %v", err)
	}
	// Exit to let it take over
	os.Exit(0)

	return nil
}

func compareVersions(v1, v2 string) int {
	// Remove 'v' prefix if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	// Pad shorter version with zeros
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int

		if i < len(parts1) {
			n1, _ = strconv.Atoi(parts1[i])
		}
		if i < len(parts2) {
			n2, _ = strconv.Atoi(parts2[i])
		}

		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}

	return 0
}
