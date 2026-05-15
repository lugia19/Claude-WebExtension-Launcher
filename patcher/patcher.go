package patcher

import (
	"bytes"
	"claude-webext-patcher/utils"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// EmbeddedFS is the embedded filesystem from the main package
var EmbeddedFS embed.FS

const (
	windowsReleasesURL = "https://downloads.claude.ai/releases/win32/x64/RELEASES"
	macosReleasesURL   = "https://downloads.claude.ai/releases/darwin/universal/RELEASES.json"
	appFolderName      = "app-latest"
	KeepNupkgFiles     = false
	PatchVersion       = "5"
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
	// Generic patch that should work for most versions
	"generic": {
		{
			Files:   []string{".vite/build/index*.js"},
			Exclude: []string{"index.pre"},
			Func: func(content []byte) []byte {
				return patch_generic(content)
			},
		},
		{
			// Patch index.pre.js to redirect userData path for instance isolation
			Files: []string{".vite/build/index.pre*.js"},
			Func: func(content []byte) []byte {
				return patch_index_pre(content)
			},
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

var (
	asarCmd       string
	jsBeautifyCmd string
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

// Patch functions
func readInjection(version, filename string) (string, error) {
	// Must use forward slashes for embed.FS, not filepath.Join
	injectionPath := "resources/injections/" + version + "/" + filename
	content, err := EmbeddedFS.ReadFile(injectionPath)
	if err != nil {
		return "", fmt.Errorf("could not load injection %s for version %s: %v", filename, version, err)
	}
	return string(content), nil
}

func readCombinedInjection(version string, filenames []string) (string, error) {
	var combined strings.Builder

	for i, filename := range filenames {
		content, err := readInjection(version, filename)
		if err != nil {
			return "", err
		}

		// Add the content
		combined.WriteString(content)

		// Add newlines between files for clarity
		if i < len(filenames)-1 {
			combined.WriteString("\n\n")
		}
	}

	return combined.String(), nil
}

func patch_index_pre(content []byte) []byte {
	contentStr := string(content)

	// Find "use strict" at top of file
	useStrictPattern := `"use strict";`
	idx := strings.Index(contentStr, useStrictPattern)
	if idx == -1 {
		fmt.Printf("Warning: Could not find \"use strict\" in index.pre file\n")
		return content
	}

	// Load the userData injection
	injection, err := readInjection("generic", "instance_userdata.js")
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
		return content
	}

	// Insert after "use strict";
	insertPos := idx + len(useStrictPattern)
	contentStr = contentStr[:insertPos] + "\n" + injection + "\n" + contentStr[insertPos:]

	return []byte(contentStr)
}

func patch_generic(content []byte) []byte {
	contentStr := string(content)

	// First, find the injection point — match any X.on("resize" and pick the
	// one that is NOT on a line containing "fullScreen" (which is a different call).
	resizePattern := regexp.MustCompile(`(\w+)\.on\("resize"`)
	allMatches := resizePattern.FindAllStringSubmatchIndex(contentStr, -1)
	var mainWindowVar string
	var injectionIndex int
	found := false
	for _, loc := range allMatches {
		lineStart := strings.LastIndex(contentStr[:loc[0]], "\n") + 1
		lineEnd := strings.Index(contentStr[loc[0]:], "\n")
		if lineEnd == -1 {
			lineEnd = len(contentStr)
		} else {
			lineEnd += loc[0]
		}
		line := contentStr[lineStart:lineEnd]
		if strings.Contains(strings.ToLower(line), "fullscreen") {
			continue
		}
		mainWindowVar = contentStr[loc[2]:loc[3]]
		injectionIndex = loc[0]
		found = true
		break
	}
	if !found {
		fmt.Printf("Warning: Could not find injection point (*.on(\"resize\") without fullScreen)\n")
		return content
	}

	fmt.Printf("Detected mainWindow variable: %s\n", mainWindowVar)

	// Find the webView variable by looking for pattern like: variableName.webContents.on("dom-ready"
	// We need to find this before the injection point
	contentBeforeInjection := contentStr[:injectionIndex]

	// Look for webView pattern
	webViewPattern := regexp.MustCompile(`(\w+)\.webContents\.on\("dom-ready"`)
	webViewMatches := webViewPattern.FindStringSubmatch(contentBeforeInjection)

	webViewVar := ""
	if webViewMatches != nil && len(webViewMatches) >= 2 {
		webViewVar = webViewMatches[1]
		fmt.Printf("Detected webView variable: %s\n", webViewVar)
	} else {
		fmt.Printf("Warning: Could not detect webView variable, falling back to 'r'\n")
		webViewVar = "r" // Fallback to common default
	}

	// Replace requestSingleInstanceLock calls with our wrapper BEFORE injecting
	// the helper function (which itself contains a requestSingleInstanceLock call)
	lockPattern := regexp.MustCompile(`\w+\.app\.requestSingleInstanceLock\(\)`)
	contentStr = lockPattern.ReplaceAllString(contentStr, "__modifiedLock()")

	// Inject instance lock helper function after "use strict"
	useStrictPattern := `"use strict";`
	useStrictIdx := strings.Index(contentStr, useStrictPattern)
	if useStrictIdx != -1 {
		lockInjection, err := readInjection("generic", "instance_lock.js")
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
		} else {
			insertPos := useStrictIdx + len(useStrictPattern)
			contentStr = contentStr[:insertPos] + "\n" + lockInjection + "\n" + contentStr[insertPos:]
		}
	} else {
		fmt.Printf("Warning: Could not find \"use strict\" for lock injection\n")
	}

	// Load all injection files
	injectionFiles := []string{
		"extension_loader.js",
		"alarm_polyfill.js",
		"notification_polyfill.js",
		"tabevents_polyfill.js",
	}

	injection, err := readCombinedInjection("generic", injectionFiles)
	if err != nil {
		fmt.Printf("Warning: %v\n", err)
		return content
	}

	// Replace placeholders in the injection with detected variable names
	injection = strings.ReplaceAll(injection, "PLACEHOLDER_MAINWINDOW", mainWindowVar)
	injection = strings.ReplaceAll(injection, "PLACEHOLDER_WEBVIEW", webViewVar)

	// Re-find the injection point (content may have shifted from earlier insertions)
	allMatches = resizePattern.FindAllStringSubmatchIndex(contentStr, -1)
	for _, loc := range allMatches {
		lineStart := strings.LastIndex(contentStr[:loc[0]], "\n") + 1
		lineEnd := strings.Index(contentStr[loc[0]:], "\n")
		if lineEnd == -1 {
			lineEnd = len(contentStr)
		} else {
			lineEnd += loc[0]
		}
		line := contentStr[lineStart:lineEnd]
		if strings.Contains(strings.ToLower(line), "fullscreen") {
			continue
		}
		// Insert after the closest preceding semicolon to avoid injecting
		// inside a comma-separated expression.
		insertPos := strings.LastIndex(contentStr[:loc[0]], ";")
		if insertPos == -1 {
			insertPos = loc[0]
		} else {
			insertPos++ // after the semicolon
		}
		contentStr = contentStr[:insertPos] + "\n" + injection + "\n" + contentStr[insertPos:]
		break
	}

	// Second injection point - add chrome-extension: to the array
	gxPattern := `["devtools:", "file:"]`
	gxReplacement := `["devtools:", "file:", "chrome-extension:"]`
	contentStr = strings.Replace(contentStr, gxPattern, gxReplacement, 1)

	return []byte(contentStr)
}

func ensureTools() error {
	// Check if node exists and get version
	cmd := exec.Command("node", "--version")
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Error: Node.js not found. Please install Node.js first.")
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		return fmt.Errorf("Node.js not found")
	}

	// Parse Node version (format: v22.0.0)
	versionStr := strings.TrimSpace(string(output))
	versionStr = strings.TrimPrefix(versionStr, "v")
	versionParts := strings.Split(versionStr, ".")
	majorVersion := 0
	if len(versionParts) > 0 {
		fmt.Sscanf(versionParts[0], "%d", &majorVersion)
	}

	fmt.Printf("Found Node.js %s\n", versionStr)

	// Set tool paths
	nodeModulesPath := utils.ResolveInstallPath(filepath.Join("node_modules", ".bin"))
	asarCmd = filepath.Join(nodeModulesPath, "asar")
	jsBeautifyCmd = filepath.Join(nodeModulesPath, "js-beautify")
	applyPlatformToolSuffix()

	// Install locally if needed
	if _, err := os.Stat(asarCmd); os.IsNotExist(err) {
		fmt.Println("Installing tools locally...")

		// Choose asar package based on Node version
		asarPackage := "asar"
		if majorVersion >= 22 {
			asarPackage = "@electron/asar"
		}

		installDir := utils.ResolveInstallPath(".")
		// Ensure a package.json exists so npm doesn't walk up into the locked WindowsApps directory
		pkgJsonPath := filepath.Join(installDir, "package.json")
		if _, err := os.Stat(pkgJsonPath); os.IsNotExist(err) {
			os.WriteFile(pkgJsonPath, []byte("{}"), 0644)
		}
		cmd := exec.Command("npm", "install", "--prefix", installDir, "--no-save", asarPackage, "js-beautify")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install tools: %v", err)
		}
	}

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

	if err := ensureTools(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
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

	// Check if version is verified
	versionVerified := IsVersionVerified(newestVersion)

	// Decide whether to update based on verification status and existing installation
	shouldUpdate := false
	if forceUpdate {
		// Force update regardless of version verification
		shouldUpdate = (currentVersion != newestVersion)
		if shouldUpdate {
			fmt.Printf("Force update enabled, updating to version %s...\n", newestVersion)
			if !versionVerified {
				fmt.Printf("WARNING: Version %s is not verified compatible.\n", newestVersion)
			}
		}
	} else if versionVerified {
		// Always update to verified versions
		shouldUpdate = (currentVersion != newestVersion)
		if shouldUpdate {
			fmt.Printf("Version %s is verified compatible, updating...\n", newestVersion)
		}
	} else {
		// Unverified version and not forcing
		if currentVersion == "" {
			// No existing installation - try the new version
			shouldUpdate = true
			fmt.Printf("WARNING: Version %s is not verified compatible, but no existing installation found.\n", newestVersion)
			fmt.Println("Attempting installation anyway...")
		} else if currentVersion == newestVersion {
			// Already on this unverified version
			shouldUpdate = false
			fmt.Printf("Already on version %s (unverified but currently installed)\n", newestVersion)
		} else {
			// Have existing installation and new version is unverified - stay on current
			shouldUpdate = false
			fmt.Printf("WARNING: Version %s is not verified compatible.\n", newestVersion)
			fmt.Printf("Keeping existing installation (version %s) to avoid potential issues.\n", currentVersion)
			fmt.Println("To force update, use --force-update flag.")
		}
	}

	// Update if needed
	patchVersionFile := filepath.Join(installBaseDir, "patch-version.txt")
	if shouldUpdate {
		fmt.Printf("Updating to %s...\n", newestVersion)

		if err := downloadAndExtract(newestVersion, downloadURL); err != nil {
			if canFallbackToExisting() {
				fmt.Printf("Warning: download/extract failed (%v), continuing with existing installation.\n", err)
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
					return nil
				}
				return err
			}
			if err := applyPatches(newestVersion); err != nil {
				if canFallbackToExisting() {
					fmt.Printf("Warning: re-patching failed (%v), continuing with existing installation.\n", err)
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

// Helper function to apply a patch to a single file
func applyPatchToFile(filePath string, content []byte, patchFunc func([]byte) []byte) bool {
	// Get the base filename for logging
	fileName := filepath.Base(filePath)

	// Beautify JS files if needed
	if strings.HasSuffix(fileName, ".js") {
		if !bytes.Contains(content, []byte("/* CLAUDE-MANAGER-BEAUTIFIED */")) {
			beautifyCmd := jsBeautifyCommand(filePath, "-o", filePath)
			if err := beautifyCmd.Run(); err != nil {
				fmt.Printf("Warning: Could not beautify %s: %v\n", fileName, err)
			} else {
				// Add marker comment
				beautifiedContent, _ := os.ReadFile(filePath)
				markedContent := append([]byte("/* CLAUDE-MANAGER-BEAUTIFIED */\n"), beautifiedContent...)
				os.WriteFile(filePath, markedContent, 0644)
				// Re-read for patching
				content = markedContent
			}
		}
	}

	// Apply the patch
	newContent := patchFunc(content)
	if err := os.WriteFile(filePath, newContent, 0644); err != nil {
		fmt.Printf("Failed to write %s: %v\n", fileName, err)
		return false
	}

	return true
}

func applyPatches(version string) error {
	// Try version-specific patches first, fall back to generic
	patches, ok := supportedVersions[version]
	if !ok || len(patches) == 0 {
		// Try generic patches
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
		// Don't fail the whole process if icons can't be replaced
	}

	asarPath := filepath.Join(appResourcesDir, "app.asar")
	tempDir := utils.ResolvePath("asar-temp")

	// Unpack asar
	fmt.Println("Unpacking asar...")
	fmt.Printf("Running command: %s\n", asarCmd)
	fmt.Printf("Arguments: extract %s %s\n", asarPath, tempDir)

	cmd := asarCommand("extract", asarPath, tempDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Command failed with error: %v\n", err)
		fmt.Printf("Output: %s\n", string(output))
		return fmt.Errorf("unpacking asar: %v\nOutput: %s", err, string(output))
	}
	fmt.Printf("Unpacking successful\n")
	defer os.RemoveAll(tempDir)

	// Apply patches
	for i, patch := range patches {
		fmt.Printf("Applying patch %d/%d...\n", i+1, len(patches))

		// Try each file pattern for this patch
		patchApplied := false
		for _, filePattern := range patch.Files {
			// Check if this is a pattern (contains *) or exact filename
			if strings.Contains(filePattern, "*") {
				// Use filepath.Glob to find matching files
				pattern := filepath.Join(tempDir, filePattern)
				matches, err := filepath.Glob(pattern)
				if err != nil {
					fmt.Printf("Error with pattern %s: %v\n", filePattern, err)
					continue
				}

				fmt.Printf("Pattern %s matched %d files\n", filePattern, len(matches))

				// Apply patch to all matching files
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
					fmt.Printf("Trying matched file: %s\n", relPath)

					content, err := os.ReadFile(matchedFile)
					if err != nil {
						fmt.Printf("  Skipping %s: %v\n", relPath, err)
						continue
					}

					if applyPatchToFile(matchedFile, content, patch.Func) {
						fmt.Printf("  Successfully applied patch to %s\n", relPath)
						patchApplied = true
					}
				}
			} else {
				// Exact filename
				filePath := filepath.Join(tempDir, filePattern)
				content, err := os.ReadFile(filePath)
				if err != nil {
					fmt.Printf("Skipping %s: %v\n", filePattern, err)
					continue
				}

				fmt.Printf("Found file: %s\n", filePattern)
				if applyPatchToFile(filePath, content, patch.Func) {
					fmt.Printf("Successfully applied patch to %s\n", filePattern)
					patchApplied = true
					break // For exact files, one success is enough
				}
			}

			if patchApplied {
				break // Move to next patch
			}
		}

		if !patchApplied {
			return fmt.Errorf("patch %d failed: none of the target files could be patched", i+1)
		}
	}

	// Backup original
	os.Rename(asarPath, asarPath+".backup")

	// Repack asar
	fmt.Println("Repacking asar...")
	fmt.Printf("Running command: %s\n", asarCmd)
	fmt.Printf("Arguments: pack %s %s\n", tempDir, asarPath)

	cmd = asarCommand("pack", tempDir, asarPath)
	output2, err2 := cmd.CombinedOutput()
	if err2 != nil {
		fmt.Printf("Command failed with error: %v\n", err2)
		fmt.Printf("Output: %s\n", string(output2))
		os.Rename(asarPath+".backup", asarPath) // Restore on failure
		return fmt.Errorf("repacking asar: %v\nOutput: %s", err2, string(output2))
	}
	fmt.Printf("Repacking successful\n")

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
