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

func UpdateAll() error {
	fmt.Println("Checking extensions...")

	// Create extensions dir if needed
	os.MkdirAll(utils.ResolvePath("web-extensions"), 0755)

	for _, ext := range extensions {
		// Check current version
		manifestPath := filepath.Join(utils.ResolvePath("web-extensions"), ext.Folder, "manifest.json")
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

	tempFile := utils.ResolvePath(folder + "-temp.zip")
	out, _ := os.Create(tempFile)
	io.Copy(out, resp.Body)
	out.Close()
	defer os.Remove(tempFile)

	// Remove old and extract new
	extPath := filepath.Join(utils.ResolvePath("web-extensions"), folder)
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
