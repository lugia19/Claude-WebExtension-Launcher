package patcher

import (
	"claude-webext-patcher/asar"
	"claude-webext-patcher/utils"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// EmbeddedFS is the embedded filesystem from the main package
var EmbeddedFS embed.FS

// Debug enables pausing on warnings/errors during patching
var Debug bool

func debugPause() {
	if Debug {
		fmt.Println("Press Enter to continue...")
		fmt.Scanln()
	}
}

const (
	// windowsMSIXRedirectURL resolves (via HTTP 307) to the latest Windows MSIX,
	// e.g. https://downloads.claude.ai/releases/win32/x64/{VERSION}/Claude-{hash}.msix.
	// The MSIX is the complete app and additionally ships the Cowork service binary
	// (cowork-svc.exe) and its sandbox image (smol-bin.x64.vhdx), which the Squirrel
	// .nupkg does not contain.
	windowsMSIXRedirectURL = "https://claude.ai/api/desktop/win32/x64/msix/latest/redirect"
	macosReleasesURL       = "https://downloads.claude.ai/releases/darwin/universal/RELEASES.json"
	appFolderName          = "app-latest"
	KeepNupkgFiles         = false
	PatchVersion           = "7"
)

type MacOSManifest struct {
	CurrentRelease string `json:"currentRelease"`
	Releases       []struct {
		Version  string `json:"version"`
		UpdateTo struct {
			URL string `json:"url"`
		} `json:"updateTo"`
	} `json:"releases"`
}

type Patch struct {
	Files   []string
	Exclude []string
	Func    func(content []byte) []byte
}

var supportedVersions = map[string][]Patch{
	// Generic patch that should work for most versions.
	// The wrapper (installed separately) handles instance isolation, multi-instance
	// lock, extension loading, and polyfills. These content patches handle things
	// that can't be done from the wrapper.
	"generic": {
		{
			Files:   []string{".vite/build/index*.js"},
			Exclude: []string{"index.pre", "wrapper"},
			Func:    patchProtocolArray,
		},
	},
	// Add version-specific overrides here when needed
}

// Cached verified versions list (loaded on first use)
var versionsVerifiedGenericCompatible []string

const verifiedVersionsURL = "https://raw.githubusercontent.com/lugia19/Claude-WebExtension-Launcher/master/resources/verified_versions.json"

var (
	AppFolder       string
	installBaseDir  string
	appResourcesDir string
	appExePath      string
)

func init() {
	initPaths()
}

func InstallBaseDir() string {
	return installBaseDir
}

// ForceRedownload deletes the version file and forces a full re-download and re-patch.
func ForceRedownload() error {
	claudeVersionFile := filepath.Join(installBaseDir, "claude-version.txt")
	os.Remove(claudeVersionFile)
	return EnsurePatched(true)
}

// Load verified versions from GitHub, with fallback to embedded JSON
func loadVerifiedVersions() []string {
	// Try fetching from GitHub first
	resp, err := http.Get(verifiedVersionsURL)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			body, err := io.ReadAll(resp.Body)
			if err == nil {
				var versions []string
				if err := json.Unmarshal(body, &versions); err == nil {
					fmt.Printf("Loaded %d verified versions from GitHub\n", len(versions))
					return versions
				}
			}
		}
	}

	// Fallback to embedded JSON
	fmt.Println("Falling back to embedded verified versions list")
	embeddedData, err := EmbeddedFS.ReadFile("resources/verified_versions.json")
	if err != nil {
		fmt.Printf("Warning: Could not load embedded verified versions: %v\n", err)
		return []string{}
	}

	var versions []string
	if err := json.Unmarshal(embeddedData, &versions); err != nil {
		fmt.Printf("Warning: Could not parse embedded verified versions: %v\n", err)
		return []string{}
	}

	fmt.Printf("Loaded %d verified versions from embedded file\n", len(versions))
	return versions
}

// Check if a version is verified to work with generic patches
func IsVersionVerified(version string) bool {
	// Load versions on first use
	if versionsVerifiedGenericCompatible == nil {
		versionsVerifiedGenericCompatible = loadVerifiedVersions()
	}

	for _, v := range versionsVerifiedGenericCompatible {
		if v == version {
			return true
		}
	}
	return false
}

func DeploySentinelExtension() error {
	sentinelDir := filepath.Join(utils.ResolveInstallPath("web-extensions"), "sentinel")
	os.MkdirAll(sentinelDir, 0755)

	for _, name := range []string{"manifest.json", "content.js"} {
		data, err := EmbeddedFS.ReadFile("resources/sentinel_extension/" + name)
		if err != nil {
			return fmt.Errorf("reading embedded sentinel file %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(sentinelDir, name), data, 0644); err != nil {
			return fmt.Errorf("writing sentinel file %s: %v", name, err)
		}
	}

	fmt.Println("Deployed sentinel extension.")
	return nil
}

// patchProtocolArray adds "chrome-extension:" to the allowed protocols array.
// Matches the prefix ["devtools:","file:" and inserts before the closing ].
func patchProtocolArray(content []byte) []byte {
	contentStr := string(content)

	prefix := `["devtools:","file:"`
	idx := strings.Index(contentStr, prefix)
	if idx == -1 {
		fmt.Println("Warning: Could not find protocol array prefix in bundle")
		debugPause()
		return content
	}

	// Find the closing ] after the prefix
	closingIdx := strings.Index(contentStr[idx:], "]")
	if closingIdx == -1 {
		fmt.Println("Warning: Could not find closing ] for protocol array")
		debugPause()
		return content
	}
	closingIdx += idx

	// Check if chrome-extension: is already present
	arrayContent := contentStr[idx : closingIdx+1]
	if strings.Contains(arrayContent, "chrome-extension:") {
		fmt.Println("Protocol array already contains chrome-extension:, skipping")
		return content
	}

	// Insert ,"chrome-extension:" before the ]
	contentStr = contentStr[:closingIdx] + `,"chrome-extension:"` + contentStr[closingIdx:]
	fmt.Println("Added chrome-extension: to protocol array")
	return []byte(contentStr)
}

// installWrapper copies the wrapper.js into the unpacked asar and redirects
// package.json to load it instead of the original entry point.
func installWrapper(tempDir string, version string) error {
	// Read and modify package.json
	pkgPath := filepath.Join(tempDir, "package.json")
	pkgData, err := os.ReadFile(pkgPath)
	if err != nil {
		return fmt.Errorf("reading package.json: %v", err)
	}

	var pkg map[string]interface{}
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		return fmt.Errorf("parsing package.json: %v", err)
	}

	originalMain, _ := pkg["main"].(string)
	if originalMain == "" {
		return fmt.Errorf("package.json has no main field")
	}
	fmt.Printf("Original main entry: %s\n", originalMain)

	pkg["main"] = ".vite/build/wrapper.js"
	pkg["_originalMain"] = originalMain

	newPkgData, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling package.json: %v", err)
	}
	if err := os.WriteFile(pkgPath, newPkgData, 0644); err != nil {
		return fmt.Errorf("writing package.json: %v", err)
	}
	fmt.Println("Redirected package.json main to wrapper.js")

	// Try version-specific wrapper first, fall back to generic
	wrapperPath := "resources/injections/" + version + "/wrapper.js"
	wrapperData, err := EmbeddedFS.ReadFile(wrapperPath)
	if err != nil {
		wrapperPath = "resources/injections/generic/wrapper.js"
		wrapperData, err = EmbeddedFS.ReadFile(wrapperPath)
		if err != nil {
			return fmt.Errorf("reading embedded wrapper.js: %v", err)
		}
		fmt.Println("Using generic wrapper.js")
	} else {
		fmt.Printf("Using version-specific wrapper.js for %s\n", version)
	}

	wrapperDst := filepath.Join(tempDir, ".vite", "build", "wrapper.js")
	if err := os.WriteFile(wrapperDst, wrapperData, 0644); err != nil {
		return fmt.Errorf("writing wrapper.js: %v", err)
	}
	fmt.Println("Installed wrapper.js")

	return nil
}

func canFallbackToExisting() bool {
	_, err := os.Stat(appExePath)
	return err == nil
}

func EnsurePatched(forceUpdate bool) error {
	if err := prepareInstallDir(); err != nil {
		return fmt.Errorf("setting up install directory: %v", err)
	}

	// Get current version (stored at installBaseDir level, not inside AppFolder)
	currentVersion := ""
	claudeVersionFile := filepath.Join(installBaseDir, "claude-version.txt")
	if data, err := os.ReadFile(claudeVersionFile); err == nil {
		currentVersion = strings.TrimSpace(string(data))
		fmt.Printf("Current version: %s\n", currentVersion)
	}

	// Get latest version and download URL
	newestVersion, downloadURL, err := GetLatestVersion()
	if err != nil {
		// If we have an existing installation, continue using it
		if currentVersion != "" {
			fmt.Printf("Warning: %v\n", err)
			fmt.Printf("Continuing with existing installation (version %s)\n", currentVersion)
			debugPause()

			// Check if the app executable exists
			if _, err := os.Stat(appExePath); os.IsNotExist(err) {
				return fmt.Errorf("existing installation is incomplete (executable not found)")
			}

			return nil // Continue with existing installation
		}
		// No existing installation and no version available
		return fmt.Errorf("no versions available and no existing installation found")
	}

	fmt.Printf("Latest version: %s\n", newestVersion)

	// Always update to the latest version
	shouldUpdate := currentVersion != newestVersion
	if shouldUpdate {
		if !IsVersionVerified(newestVersion) {
			fmt.Printf("Note: Version %s has not been explicitly verified, but should work fine.\n", newestVersion)
			fmt.Println("If you run into issues, let me know on GitHub.")
		}
	}

	// Update if needed
	patchVersionFile := filepath.Join(installBaseDir, "patch-version.txt")
	if shouldUpdate {
		fmt.Printf("Updating to %s...\n", newestVersion)

		if err := downloadAndExtract(newestVersion, downloadURL); err != nil {
			if canFallbackToExisting() {
				fmt.Printf("Warning: download/extract failed (%v), continuing with existing installation.\n", err)
				debugPause()
				return nil
			}
			return err
		}

		// Write version
		os.WriteFile(claudeVersionFile, []byte(newestVersion), 0644)

		// Apply patches
		if err := applyPatches(newestVersion); err != nil {
			if canFallbackToExisting() {
				fmt.Printf("Warning: patching failed (%v), continuing with existing installation.\n", err)
				debugPause()
				return nil
			}
			return fmt.Errorf("applying patches: %v", err)
		}
		os.WriteFile(patchVersionFile, []byte(PatchVersion), 0644)
	} else {
		if currentVersion == newestVersion {
			fmt.Println("Already on the latest version")
		}

		// Check if injection code needs updating
		currentPatchVersion := ""
		if data, err := os.ReadFile(patchVersionFile); err == nil {
			currentPatchVersion = strings.TrimSpace(string(data))
		}
		if currentPatchVersion != PatchVersion {
			fmt.Printf("Patch version changed (%s -> %s), re-downloading and re-patching...\n", currentPatchVersion, PatchVersion)
			if err := downloadAndExtract(newestVersion, downloadURL); err != nil {
				if canFallbackToExisting() {
					fmt.Printf("Warning: re-download failed (%v), continuing with existing installation.\n", err)
					debugPause()
					return nil
				}
				return err
			}
			if err := applyPatches(newestVersion); err != nil {
				if canFallbackToExisting() {
					fmt.Printf("Warning: re-patching failed (%v), continuing with existing installation.\n", err)
					debugPause()
					return nil
				}
				return fmt.Errorf("applying patches: %v", err)
			}
			os.WriteFile(claudeVersionFile, []byte(newestVersion), 0644)
			os.WriteFile(patchVersionFile, []byte(PatchVersion), 0644)
		}
	}

	return nil
}

func applyPatches(version string) error {
	// Try version-specific patches first, fall back to generic
	patches, ok := supportedVersions[version]
	if !ok || len(patches) == 0 {
		patches, ok = supportedVersions["generic"]
		if !ok || len(patches) == 0 {
			fmt.Printf("No patches available for version %s (and no generic patches found)\n", version)
			return nil
		}
		fmt.Printf("Using generic patches for version %s\n", version)
	} else {
		fmt.Printf("Using version-specific patches for %s\n", version)
	}

	fmt.Println("Applying patches...")
	if err := replaceIcons(); err != nil {
		fmt.Printf("Warning: Could not replace icons: %v\n", err)
		debugPause()
	}

	asarPath := filepath.Join(appResourcesDir, "app.asar")
	tempDir := utils.ResolvePath("asar-temp")

	// Unpack asar
	fmt.Println("Unpacking asar...")
	if err := asar.Extract(asarPath, tempDir); err != nil {
		fmt.Printf("Unpacking failed: %v\n", err)
		return fmt.Errorf("unpacking asar: %v", err)
	}
	fmt.Println("Unpacking successful")
	defer os.RemoveAll(tempDir)

	// Install the wrapper (redirects package.json entry point)
	if err := installWrapper(tempDir, version); err != nil {
		return fmt.Errorf("installing wrapper: %v", err)
	}

	// Apply content patches (e.g. protocol array)
	for i, patch := range patches {
		fmt.Printf("Applying content patch %d/%d...\n", i+1, len(patches))

		patchApplied := false
		for _, filePattern := range patch.Files {
			pattern := filepath.Join(tempDir, filePattern)
			matches, err := filepath.Glob(pattern)
			if err != nil {
				fmt.Printf("Error with pattern %s: %v\n", filePattern, err)
				continue
			}

			for _, matchedFile := range matches {
				baseName := filepath.Base(matchedFile)
				excluded := false
				for _, ex := range patch.Exclude {
					if strings.Contains(baseName, ex) {
						excluded = true
						break
					}
				}
				if excluded {
					continue
				}

				relPath, _ := filepath.Rel(tempDir, matchedFile)
				fmt.Printf("Patching %s\n", relPath)

				content, err := os.ReadFile(matchedFile)
				if err != nil {
					fmt.Printf("  Skipping %s: %v\n", relPath, err)
					continue
				}

				newContent := patch.Func(content)
				if err := os.WriteFile(matchedFile, newContent, 0644); err != nil {
					fmt.Printf("  Failed to write %s: %v\n", relPath, err)
					continue
				}
				patchApplied = true
			}

			if patchApplied {
				break
			}
		}

		if !patchApplied {
			fmt.Printf("Warning: content patch %d did not match any files\n", i+1)
			debugPause()
		}
	}

	// Backup original and repack
	os.Rename(asarPath, asarPath+".backup")

	fmt.Println("Repacking asar...")
	if err := asar.Pack(tempDir, asarPath); err != nil {
		fmt.Printf("Repacking failed: %v\n", err)
		os.Rename(asarPath+".backup", asarPath)
		return fmt.Errorf("repacking asar: %v", err)
	}
	fmt.Println("Repacking successful")

	if err := finalizePatches(); err != nil {
		return err
	}

	fmt.Println("Patches applied successfully!")
	return nil
}

func replaceIcons() error {
	fmt.Println("Replacing icons...")

	replacePlatformAppIcon()

	// Copy other icons (works for all platforms)
	iconEntries, err := EmbeddedFS.ReadDir("resources/icons")
	if err != nil {
		return err
	}

	for _, entry := range iconEntries {
		if entry.IsDir() {
			continue
		}

		// Must use forward slashes for embed.FS
		iconPath := "resources/icons/" + entry.Name()
		dst := filepath.Join(appResourcesDir, entry.Name())

		fmt.Printf("  %s -> %s\n", entry.Name(), dst)

		input, err := EmbeddedFS.ReadFile(iconPath)
		if err != nil {
			return err
		}

		if err := os.WriteFile(dst, input, 0644); err != nil {
			return err
		}
	}

	return nil
}
