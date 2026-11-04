package extensions

import (
	"archive/zip"
	"claude-webext-patcher/utils"
	"encoding/json"
	"fmt"
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

// isLinked reports whether the extension's folder, or web-extensions itself, is a junction
// or symlink: a developer's own checkout, left alone since updating would replace it.
func isLinked(ext Extension) bool {
	root := utils.ResolveInstallPath("web-extensions")
	for _, p := range []string{root, filepath.Join(root, ext.Folder)} {
		if _, err := os.Readlink(p); err == nil {
			return true
		}
	}
	return false
}

func fetchLatestRelease(ext Extension) (*extensionRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", ext.Owner, ext.Repo)
	resp, err := utils.GetURL(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
		if isLinked(ext) {
			continue
		}
		stages, err := extensionUpdateDirs(filepath.Join(utils.ResolveInstallPath("web-extensions"), ext.Folder))
		if err != nil {
			return false, err
		}
		if len(stages) > 0 {
			return true, nil
		} // recovery needs the worker, even if already up to date
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
		if isLinked(ext) {
			fmt.Printf("  %s: linked folder, not updating\n", ext.Folder)
			continue
		}
		if err := recoverExtensionUpdates(filepath.Join(utils.ResolveInstallPath("web-extensions"), ext.Folder)); err != nil {
			fmt.Printf("  %s: recovery failed: %v\n", ext.Folder, err)
			failed = append(failed, ext.Folder)
			continue
		}

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
	return installExtension(url, filepath.Join(utils.ResolveInstallPath("web-extensions"), folder))
}

// installExtension stages and validates the whole update before moving the old
// extension aside. Both renames stay on the same filesystem, and a failed swap
// restores the previous extension instead of leaving an empty or partial folder.
func installExtension(url, destination string) error {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	if err := recoverExtensionUpdates(destination); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(destination)+"-update-*")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(stage)
		}
	}()
	if err := markExtensionUpdate(stage, destination); err != nil {
		return err
	}
	tempFile := filepath.Join(stage, "download.zip")
	if err := utils.DownloadFile(url, tempFile, nil); err != nil {
		return err
	}
	extPath := filepath.Join(stage, "extension")
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

	if err := validateManifest(extPath); err != nil {
		return err
	}

	backup := filepath.Join(stage, "previous")
	hadPrevious := false
	if _, err := os.Lstat(destination); err == nil {
		// A linked developer checkout must never be replaced, even if the link
		// appeared after UpdateAll's earlier check.
		if _, err := os.Readlink(destination); err == nil {
			return fmt.Errorf("refusing to replace linked extension %s", destination)
		}
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("moving previous extension aside: %w", err)
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(extPath, destination); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backup, destination); restoreErr != nil {
				// Keep the backup for recovery instead of deleting it with stage.
				cleanup = false
				return fmt.Errorf("installing extension: %v; restoring it: %v (backup: %s)", err, restoreErr, backup)
			}
		}
		return fmt.Errorf("installing extension: %w", err)
	}
	return nil
}

func validateManifest(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return fmt.Errorf("reading extension manifest: %w", err)
	}
	var manifest struct {
		ManifestVersion int    `json:"manifest_version"`
		Name            string `json:"name"`
		Version         string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing extension manifest: %w", err)
	}
	if (manifest.ManifestVersion != 2 && manifest.ManifestVersion != 3) || strings.TrimSpace(manifest.Name) == "" || manifest.Version == "" {
		return fmt.Errorf("extension manifest must contain manifest_version (2 or 3), name and version")
	}
	if !validExtensionVersion(manifest.Version) {
		return fmt.Errorf("invalid extension version %q", manifest.Version)
	}
	return nil
}

// Chromium accepts one to four canonical decimal components in [0, 65535],
// with at least one nonzero component.
func validExtensionVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) > 4 {
		return false
	}
	nonzero := false
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 65535 || strconv.Itoa(n) != part {
			return false
		}
		nonzero = nonzero || n != 0
	}
	return nonzero
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
