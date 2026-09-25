package patcher

import (
	"claude-webext-patcher/utils"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

// linuxAptBase is Anthropic's official APT repository for Claude Desktop. The Packages
// index lists every published .deb along with its SHA-256.
const linuxAptBase = "https://downloads.claude.ai/claude-desktop/apt/stable"

// linuxDebPrefix is where the .deb installs the app; that subtree becomes AppFolder.
const linuxDebPrefix = "usr/lib/claude-desktop"

// debSHA256ByURL holds the SHA-256 the Packages index published for each .deb URL
// returned by GetLatestVersion, so downloadAndExtract can verify what it fetched.
var debSHA256ByURL = map[string]string{}

// setAppPaths points the app-folder globals at appFolder. The .deb uses a flat
// Electron layout: the claude-desktop binary with resources/ beside it.
func setAppPaths(appFolder string) {
	AppFolder = appFolder
	appResourcesDir = filepath.Join(appFolder, "resources")
	appExePath = filepath.Join(appFolder, "claude-desktop")
}

// finalizePatches is a no-op on Linux: Electron does not enforce asar integrity there
// and the app is not code-signed.
func finalizePatches() error {
	return nil
}

// replacePlatformAppIcon is a no-op on Linux; the window icon comes from resources/,
// which replaceIcons already covers.
func replacePlatformAppIcon() {}

func GetLatestVersion() (string, string, error) {
	// Debian and Go agree on the names that matter here: amd64 and arm64.
	arch := runtime.GOARCH
	indexURL := fmt.Sprintf("%s/dists/stable/main/binary-%s/Packages", linuxAptBase, arch)
	fmt.Printf("Getting latest version for OS: linux (%s)\n", arch)
	fmt.Printf("Fetching Packages index from: %s\n", indexURL)

	resp, err := http.Get(indexURL)
	if err != nil {
		return "", "", fmt.Errorf("fetching Packages index: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("fetching Packages index: HTTP %d", resp.StatusCode)
	}

	pkg, err := parseAptPackages(resp.Body, "claude-desktop")
	if err != nil {
		return "", "", err
	}

	url := linuxAptBase + "/" + pkg.Filename
	debSHA256ByURL[url] = pkg.SHA256
	return pkg.Version, url, nil
}

// Prefetch downloads the Claude .deb and verifies it against the SHA-256 the Packages
// index published (GetLatestVersion must have run in this process). Returns its path.
func Prefetch(version, url string) (string, error) {
	expectedSHA := debSHA256ByURL[url]
	if expectedSHA == "" {
		return "", fmt.Errorf("no published SHA-256 for %s; refusing to install", url)
	}
	path := utils.ResolvePath(fmt.Sprintf("Claude-%s-%d.deb", version, os.Getpid()))
	if err := downloadFile(url, path); err != nil {
		return "", err
	}
	if err := verifySHA256(path, expectedSHA); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// downloadAndExtract extracts the .deb the launcher downloaded and verified. The
// worker runs as the same user, so it can use the file as-is.
func downloadAndExtract(version, downloadURL string) error {
	debPath := PrefetchedPackage
	if debPath == "" || PrefetchedVersion != version {
		return fmt.Errorf("no downloaded package for Claude %s", version)
	}

	fmt.Println("Extracting...")
	os.RemoveAll(AppFolder)
	if err := os.MkdirAll(AppFolder, 0755); err != nil {
		return err
	}
	if err := extractDebSubtree(debPath, linuxDebPrefix, AppFolder); err != nil {
		return fmt.Errorf("extracting .deb: %v", err)
	}
	if _, err := os.Stat(filepath.Join(appResourcesDir, "app.asar")); err != nil {
		return fmt.Errorf("extracted .deb has no resources/app.asar (layout changed?)")
	}
	return nil
}

func verifySHA256(filePath, expected string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing %s: %v", filepath.Base(filePath), err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		return fmt.Errorf("SHA-256 mismatch for %s (expected %s, got %s)", filepath.Base(filePath), expected, got)
	}
	fmt.Println("Verified SHA-256 of downloaded .deb")
	return nil
}
