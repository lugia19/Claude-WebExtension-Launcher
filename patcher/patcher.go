package patcher

import (
	"claude-webext-patcher/asar"
	"claude-webext-patcher/utils"
	"embed"
	"encoding/json"
	"fmt"
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
	// windowsMSIXRedirectURLFmt resolves (via HTTP 307) to the latest Windows MSIX for a given
	// arch ("x64" or "arm64"), e.g. https://downloads.claude.ai/releases/win32/x64/{VERSION}/Claude-{hash}.msix.
	// The MSIX is the complete app and additionally ships the Cowork service binary
	// (cowork-svc.exe) and its sandbox image (smol-bin.{arch}.vhdx), which the Squirrel
	// .nupkg does not contain. The arch is the native host arch (see HostArch), so an
	// emulated amd64 launcher on ARM64 still provisions native arm64 Claude.
	windowsMSIXRedirectURLFmt = "https://claude.ai/api/desktop/win32/%s/msix/latest/redirect"
	appFolderName             = "app-latest"
	KeepDownloadedArchive     = false
	PatchVersion              = "10"
)

type Patch struct {
	Files   []string
	Exclude []string
	// Func returns the (possibly modified) content and whether it actually
	// changed anything. A false modified return means "not applicable to this
	// file" — the caller keeps looking through the remaining matched files.
	Func func(content []byte) (result []byte, modified bool)
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
// The Windows/macOS bundles quote the entries with double quotes; the Linux bundle
// uses template-literal backticks ([`devtools:`,`file:`,...]), so both are tried and
// the inserted entry reuses whichever quote matched.
//
// Claude's bundle is heavily code-split, so this runs against many chunk files
// and only one contains the array. "Prefix not found" is therefore the normal
// case for most files and is reported as (content, false) without any warning —
// the caller decides whether a genuine failure occurred (no chunk matched).
func patchProtocolArray(content []byte) ([]byte, bool) {
	contentStr := string(content)

	for _, q := range []string{`"`, "`"} {
		prefix := "[" + q + "devtools:" + q + "," + q + "file:" + q
		idx := strings.Index(contentStr, prefix)
		if idx == -1 {
			continue
		}

		// Find the closing ] after the prefix
		closingIdx := strings.Index(contentStr[idx:], "]")
		if closingIdx == -1 {
			fmt.Println("Warning: Could not find closing ] for protocol array")
			debugPause()
			return content, false
		}
		closingIdx += idx

		// Check if chrome-extension: is already present
		arrayContent := contentStr[idx : closingIdx+1]
		if strings.Contains(arrayContent, "chrome-extension:") {
			fmt.Println("Protocol array already contains chrome-extension:, skipping")
			return content, false
		}

		// Insert ,"chrome-extension:" before the ]
		contentStr = contentStr[:closingIdx] + "," + q + "chrome-extension:" + q + contentStr[closingIdx:]
		fmt.Println("Added chrome-extension: to protocol array")
		return []byte(contentStr), true
	}

	return content, false
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

const stagingSuffix = ".staging"

// buildAndSwap downloads and patches a fresh Claude into a staging folder, then
// atomically swaps it into place as AppFolder. The live install is never touched
// until the final swap, so a failed/interrupted patch (or a locked claude.exe held
// by a running instance) leaves the previous working install intact rather than a
// half-written or stock one. On return the app-folder globals point back at the real
// install, so canFallbackToExisting() checks the right place.
func buildAndSwap(version, downloadURL string) error {
	realApp := AppFolder
	staging := realApp + stagingSuffix

	os.RemoveAll(staging) // clear any leftover from a previous aborted run
	setAppPaths(staging)  // build everything into the staging folder
	defer setAppPaths(realApp)

	if err := downloadAndExtract(version, downloadURL); err != nil {
		os.RemoveAll(staging)
		return err
	}
	if err := applyPatches(version); err != nil {
		os.RemoveAll(staging)
		return err
	}
	if err := swapAppFolder(staging, realApp); err != nil {
		os.RemoveAll(staging)
		return err
	}
	return nil
}

// swapAppFolder replaces target with staging via two renames (os.Rename cannot
// atomically replace an existing directory on Windows). The window where target is
// absent is a couple of fast metadata ops. If moving the old install aside fails
// (e.g. a running claude.exe holds a lock), the old install is left in place.
func swapAppFolder(staging, target string) error {
	backup := target + ".old"
	os.RemoveAll(backup)

	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return fmt.Errorf("moving old install aside (is Claude still running?): %w", err)
		}
	}

	if err := os.Rename(staging, target); err != nil {
		// Roll the old install back so we're never left without one.
		if _, berr := os.Stat(backup); berr == nil {
			os.Rename(backup, target)
		}
		return fmt.Errorf("swapping in new install: %w", err)
	}

	os.RemoveAll(backup)
	return nil
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

	// Update to the latest version, or rebuild in place when forced (the
	// --force-update recovery path re-applies a lost/corrupted patch even when the
	// Claude version is unchanged).
	versionChanged := currentVersion != newestVersion
	shouldUpdate := forceUpdate || versionChanged

	patchVersionFile := filepath.Join(installBaseDir, "patch-version.txt")
	if shouldUpdate {
		if versionChanged {
			fmt.Printf("Updating to %s...\n", newestVersion)
		} else {
			fmt.Printf("Re-applying patch for %s...\n", newestVersion)
		}

		if err := buildAndSwap(newestVersion, downloadURL); err != nil {
			if canFallbackToExisting() {
				fmt.Printf("Warning: update failed (%v), continuing with existing installation.\n", err)
				debugPause()
				return nil
			}
			return err
		}
		// Record success only after the new install is fully built and swapped in,
		// so an interrupted patch can never leave a stale "patched" marker on a stock app.
		os.WriteFile(claudeVersionFile, []byte(newestVersion), 0644)
		os.WriteFile(patchVersionFile, []byte(PatchVersion), 0644)
	} else {
		fmt.Println("Already on the latest version")

		// Injection code changed but the Claude version didn't — re-patch.
		currentPatchVersion := ""
		if data, err := os.ReadFile(patchVersionFile); err == nil {
			currentPatchVersion = strings.TrimSpace(string(data))
		}
		if currentPatchVersion != PatchVersion {
			fmt.Printf("Patch version changed (%s -> %s), re-patching...\n", currentPatchVersion, PatchVersion)
			if err := buildAndSwap(newestVersion, downloadURL); err != nil {
				if canFallbackToExisting() {
					fmt.Printf("Warning: re-patching failed (%v), continuing with existing installation.\n", err)
					debugPause()
					return nil
				}
				return err
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

				content, err := os.ReadFile(matchedFile)
				if err != nil {
					fmt.Printf("  Skipping %s: %v\n", relPath, err)
					continue
				}

				newContent, modified := patch.Func(content)
				if !modified {
					continue
				}

				fmt.Printf("Patched %s\n", relPath)
				if err := os.WriteFile(matchedFile, newContent, 0644); err != nil {
					fmt.Printf("  Failed to write %s: %v\n", relPath, err)
					continue
				}
				patchApplied = true
				break
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
