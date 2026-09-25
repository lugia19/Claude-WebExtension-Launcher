package extensions

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

type Extension struct {
	Owner  string // GitHub owner
	Repo   string // GitHub repo name
	Folder string // Local folder name in extensions/
}

var extensions = []Extension{
	{Owner: "lugia19", Repo: "Claude-Usage-Extension", Folder: "usage-tracker"},
	{Owner: "lugia19", Repo: "Claude-Toolbox", Folder: "userscript-toolbox"},
}

type extensionRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func getInstalledVersion(ext Extension) string {
	manifestPath := filepath.Join(utils.ResolveInstallPath("web-extensions"), ext.Folder, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &manifest) == nil {
		return manifest.Version
	}
	return ""
}

func fetchLatestRelease(ext Extension) (*extensionRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", ext.Owner, ext.Repo)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Error replies (e.g. a 403 rate limit) are JSON too and would otherwise decode into
	// an empty release that looks like "no update".
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	var release extensionRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("GitHub returned a release without a tag")
	}
	return &release, nil
}

// NeedsUpdate checks whether any extension has a newer version available
// without downloading anything. Used by the launcher to decide whether to run the
// worker. err is set when some lookup failed (e.g. GitHub unreachable or rate
// limited), so "no update found" can be told apart from "couldn't check".
func NeedsUpdate() (bool, error) {
	var failed []string
	for _, ext := range extensions {
		currentVersion := getInstalledVersion(ext)
		release, err := fetchLatestRelease(ext)
		if err != nil {
			fmt.Printf("  %s: error checking: %v\n", ext.Folder, err)
			failed = append(failed, ext.Folder)
			continue
		}
		releaseVersion := strings.TrimPrefix(release.TagName, "v")
		if compareVersions(currentVersion, releaseVersion) < 0 {
			return true, nil
		}
	}
	if len(failed) > 0 {
		return false, fmt.Errorf("couldn't check %s for updates", strings.Join(failed, ", "))
	}
	return false, nil
}

func UpdateAll() error {
	fmt.Println("Checking extensions...")

	// Create extensions dir if needed
	os.MkdirAll(utils.ResolveInstallPath("web-extensions"), 0755)

	var failed []string
	for _, ext := range extensions {
		currentVersion := getInstalledVersion(ext)

		release, err := fetchLatestRelease(ext)
		if err != nil {
			fmt.Printf("  %s: error checking: %v\n", ext.Folder, err)
			failed = append(failed, ext.Folder)
			continue
		}

		releaseVersion := strings.TrimPrefix(release.TagName, "v")

		if compareVersions(currentVersion, releaseVersion) >= 0 {
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
			failed = append(failed, ext.Folder)
			continue
		}

		fmt.Printf("  %s: updating %s -> %s\n", ext.Folder, currentVersion, release.TagName)

		// Download and extract
		if err := downloadAndExtractExtension(downloadURL, ext.Folder); err != nil {
			fmt.Printf("  %s: error updating: %v\n", ext.Folder, err)
			failed = append(failed, ext.Folder)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("couldn't update %s (see the log)", strings.Join(failed, ", "))
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

	// Install path, not launcher-local: on Windows this runs elevated, and the
	// launcher-local folder is user-writable.
	tempFile := utils.ResolveInstallPath(folder + "-temp.zip")
	out, _ := os.Create(tempFile)
	io.Copy(out, resp.Body)
	out.Close()
	defer os.Remove(tempFile)

	// Remove old and extract new
	extPath := filepath.Join(utils.ResolveInstallPath("web-extensions"), folder)
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
			os.MkdirAll(path, 0755)
			continue
		}

		os.MkdirAll(filepath.Dir(path), 0755)

		if err := utils.ExtractZipFile(f, path); err != nil {
			return fmt.Errorf("extracting %s: %v", f.Name, err)
		}
	}

	return nil
}

func compareVersions(v1, v2 string) int {
	// Split versions and pad to same length
	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	// Make both same length
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}

	for i := 0; i < maxLen; i++ {
		// Get digit or 0 if missing
		digit1 := 0
		if i < len(parts1) {
			digit1, _ = strconv.Atoi(parts1[i])
		}

		digit2 := 0
		if i < len(parts2) {
			digit2, _ = strconv.Atoi(parts2[i])
		}

		// Compare
		if digit1 < digit2 {
			return -1
		}
		if digit1 > digit2 {
			return 1
		}
	}

	return 0 // Equal
}
