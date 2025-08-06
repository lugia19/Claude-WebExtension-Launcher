package selfupdate

import (
	"archive/zip"
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
	data, err := os.ReadFile("version.txt")
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
	case "linux":
		return "-linux"
	default:
		return "-" + runtime.GOOS
	}
}

// getExecutableName returns the expected executable name for the current platform
func getExecutableName() string {
	execName := "claude-webext-patcher"
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

		// Launch the original
		cmd := exec.Command(originalExe)
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

		// Launch the original
		cmd := exec.Command(originalExe)
		cmd.Start()

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
	tempZip := "update-temp.zip"
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
	defer os.Remove(tempZip)

	// Extract to temp dir
	fmt.Println("Extracting update...")
	tempDir := "update-temp"
	os.RemoveAll(tempDir)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Extract zip
	zipReader, err := zip.OpenReader(tempZip)
	if err != nil {
		return fmt.Errorf("failed to open zip: %v", err)
	}

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
	if err := os.Rename(filepath.Join(tempDir, "resources"), "resources"); err != nil {
		// Resources folder might not exist in all releases
		fmt.Printf("Note: No resources folder in update\n")
	}

	// Update version.txt
	versionData, err := os.ReadFile(filepath.Join(tempDir, "version.txt"))
	if err == nil {
		os.WriteFile("version.txt", versionData, 0644)
	}

	// Copy new executable with platform-specific naming
	exePath, _ := os.Executable()
	executableName := getExecutableName()

	var newExeName string
	if runtime.GOOS == "windows" {
		newExeName = strings.TrimSuffix(filepath.Base(exePath), ".exe") + ".new.exe"
	} else {
		newExeName = filepath.Base(exePath) + ".new"
	}

	// Look for the executable in the temp dir
	newExeData, err := os.ReadFile(filepath.Join(tempDir, executableName))
	if err != nil {
		return fmt.Errorf("failed to find executable in update: %v", err)
	}

	if err := os.WriteFile(newExeName, newExeData, 0755); err != nil {
		return fmt.Errorf("failed to write new executable: %v", err)
	}

	fmt.Println("Restarting to complete update...")

	// Launch the new exe
	cmd := exec.Command("./" + newExeName)
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
