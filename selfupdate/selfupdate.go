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

func FinishUpdateIfNeeded() {
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

func CheckAndUpdate() error {
	fmt.Println("Checking for installer updates...")

	currentVersion := currentVersion()

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

	// Check if we got a valid version
	if latestVersion == "" {
		return fmt.Errorf("failed to get latest version from GitHub")
	}

	if compareVersions(currentVersion, latestVersion) >= 0 {
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
