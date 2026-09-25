package selfupdate

import (
	"archive/zip"
	"claude-webext-patcher/utils"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CurrentVersion is set by the main package to the embedded version string.
var CurrentVersion string

type releaseAsset struct {
	Name        string
	DownloadURL string
}

// FinishUpdateIfNeeded dispatches to the platform-specific update completion logic.
func FinishUpdateIfNeeded() {
	exePath, _ := os.Executable()
	finishUpdateIfNeeded(exePath)
}

func CheckAndUpdate() error {
	fmt.Println("Checking for installer updates...")

	currentVer := CurrentVersion

	// Check latest release
	url := "https://api.github.com/repos/lugia19/Claude-WebExtension-Launcher/releases/latest"
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

	// Platform-specific asset selection
	assets := make([]releaseAsset, len(release.Assets))
	for i, a := range release.Assets {
		assets[i] = releaseAsset{Name: a.Name, DownloadURL: a.DownloadURL}
	}

	downloadURL, assetName, err := selectAsset(assets)
	if err != nil {
		fmt.Println("Available releases:")
		for _, asset := range assets {
			if strings.HasSuffix(asset.Name, ".zip") {
				fmt.Printf("  - %s\n", asset.Name)
			}
		}
		return err
	}
	_ = assetName

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

		// Treat as directory if IsDir() returns true OR if it's a zero-byte entry ending with slash/backslash
		isDirectory := f.FileInfo().IsDir() || (f.UncompressedSize64 == 0 && (strings.HasSuffix(normalizedName, "/") || strings.HasSuffix(f.Name, "\\")))

		if isDirectory {
			fmt.Printf("Creating directory: %s\n", path)
			os.MkdirAll(path, 0755)
			continue
		}

		// Skip if path already exists as a directory (created by earlier MkdirAll)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			fmt.Printf("Skipping %s - already exists as directory\n", path)
			continue
		}

		os.MkdirAll(filepath.Dir(path), 0755)

		// A truncated or corrupt download must abort the update here (the zip reader
		// reports CRC mismatches from Read), not reach installUpdate as a partial file.
		if err := utils.ExtractZipFile(f, path); err != nil {
			zipReader.Close()
			os.Remove(tempZip)
			os.RemoveAll(tempDir)
			return fmt.Errorf("failed to extract %s: %v", f.Name, err)
		}
	}
	zipReader.Close()

	fmt.Println("Installing update...")
	return installUpdate(tempDir, tempZip)
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
