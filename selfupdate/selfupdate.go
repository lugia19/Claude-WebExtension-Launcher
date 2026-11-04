package selfupdate

import (
	"claude-webext-patcher/utils"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CurrentVersion is set by the main package to the embedded version string.
var CurrentVersion string

type releaseAsset struct {
	Name        string
	DownloadURL string
}

// startedFrom identifies the launcher binary this process was started from, so a
// launcher that waited for the update lock can tell another one already replaced it.
var startedFrom os.FileInfo

func init() {
	if exe, err := os.Executable(); err == nil {
		startedFrom, _ = os.Stat(exe)
	}
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
	resp, err := utils.GetURL(url)
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

	// Serialize the rest: launchers started together would otherwise share the
	// update-temp files and the platform's staging paths for the new binary.
	unlock, locked := lockUpdate()
	if !locked {
		return fmt.Errorf("another launcher is installing an update; skipping")
	}
	defer unlock()

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
	if err := utils.DownloadFile(downloadURL, tempZip, nil); err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer os.Remove(tempZip)

	fmt.Println("Extracting update...")
	tempDir := utils.ResolvePath("update-temp")
	if err := os.RemoveAll(tempDir); err != nil {
		return fmt.Errorf("clearing update directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	if err := utils.ExtractZip(tempZip, tempDir); err != nil {
		return fmt.Errorf("extracting update: %w", err)
	}

	fmt.Println("Installing update...")
	return installUpdate(tempDir, tempZip)
}

// CompareVersions compares two version strings ("1.2.3", optionally "v"-prefixed):
// negative if v1 is older, 0 if equal, positive if newer.
func CompareVersions(v1, v2 string) int {
	return compareVersions(v1, v2)
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
