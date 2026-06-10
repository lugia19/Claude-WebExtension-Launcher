//go:build windows

package patcher

import (
	"archive/zip"
	"claude-webext-patcher/utils"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func initPaths() {
	AppFolder = utils.ResolveInstallPath(appFolderName)
	installBaseDir = utils.ResolveInstallPath(".")
	appResourcesDir = filepath.Join(AppFolder, "resources")
	appExePath = filepath.Join(AppFolder, "claude.exe")
}

// TakeWindowsAppsOwnership grants Administrators read/traverse/create access on the WindowsApps directory.
// Call this early in the program, and ReleaseWindowsAppsOwnership before launching Claude.
func TakeWindowsAppsOwnership() error {
	windowsAppsDir := filepath.Dir(installBaseDir)
	cmds := []struct {
		name string
		args []string
	}{
		{"takeown", []string{"/F", windowsAppsDir}},
		{"icacls", []string{windowsAppsDir, "/grant:r", "*S-1-5-32-544:(RX,AD)"}},
	}
	for _, c := range cmds {
		cmd := exec.Command(c.name, c.args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s failed: %v\n%s", c.name, err, string(output))
		}
	}
	return nil
}

// ReleaseWindowsAppsOwnership removes our added permissions and restores TrustedInstaller as owner.
func ReleaseWindowsAppsOwnership() {
	windowsAppsDir := filepath.Dir(installBaseDir)
	cmds := []struct {
		name string
		args []string
	}{
		{"icacls", []string{windowsAppsDir, "/remove:g", "*S-1-5-32-544"}},
		{"icacls", []string{windowsAppsDir, "/setowner", "NT SERVICE\\TrustedInstaller"}},
	}
	for _, c := range cmds {
		cmd := exec.Command(c.name, c.args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Warning: cleanup step '%s' failed: %v\n%s\n", c.name, err, string(output))
			debugPause()
		}
	}
}

// ensureWindowsAppsFolder creates our subfolder in WindowsApps with proper permissions.
// Assumes TakeWindowsAppsOwnership has already been called.
func ensureWindowsAppsFolder() error {
	// Check if our folder already exists and is writable
	testFile := filepath.Join(installBaseDir, ".write-test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err == nil {
		os.Remove(testFile)
		return nil // Already exists and writable
	}

	fmt.Println("Setting up install directory in WindowsApps...")

	// Create our subfolder
	if err := os.MkdirAll(installBaseDir, 0755); err != nil {
		return fmt.Errorf("creating install directory: %v", err)
	}

	// Grant full control on our subfolder (recursive, inheritable)
	cmd := exec.Command("icacls", installBaseDir, "/grant:r", "*S-1-5-32-544:(OI)(CI)F")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("setting permissions on install dir: %v\n%s", err, string(output))
	}

	fmt.Println("Install directory created successfully.")
	return nil
}

// deployDLL extracts the embedded version.dll next to claude.exe.
func deployDLL() error {
	dllData, err := EmbeddedFS.ReadFile("resources/version.dll")
	if err != nil {
		return fmt.Errorf("reading embedded version.dll: %v", err)
	}

	dllPath := filepath.Join(AppFolder, "version.dll")
	if err := os.WriteFile(dllPath, dllData, 0755); err != nil {
		return fmt.Errorf("writing version.dll: %v", err)
	}

	fmt.Println("Deployed version.dll")
	return nil
}

func prepareInstallDir() error {
	return ensureWindowsAppsFolder()
}

func finalizePatches() error {
	// On Windows, deploy the proxy DLL (it handles integrity patching at runtime)
	if err := deployDLL(); err != nil {
		return fmt.Errorf("deploying DLL: %v", err)
	}
	return nil
}

// GrantUserReadAccess grants BUILTIN\Users read/execute on the install directory
// so the unelevated launcher can read version files and execute claude.exe.
func GrantUserReadAccess() {
	cmd := exec.Command("icacls", installBaseDir, "/grant:r", "*S-1-5-32-545:(OI)(CI)RX")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("Warning: failed to grant user read access: %v\n%s\n", err, string(output))
		debugPause()
	}
}

func replacePlatformAppIcon() {
	// Skip exe icon replacement to preserve code signature
	fmt.Println("  Skipping exe icon replacement (preserving signature)")
}

func GetLatestVersion() (string, string, error) {
	fmt.Println("Getting latest version for OS: windows")

	resp, err := http.Get(windowsReleasesURL)
	if err != nil {
		return "", "", fmt.Errorf("fetching Windows releases: %v", err)
	}
	defer resp.Body.Close()

	releasesText, _ := io.ReadAll(resp.Body)

	lines := strings.Split(string(releasesText), "\n")

	// Find the latest version from the RELEASES file
	// The format is typically: SHA1 filename size
	var versions []struct {
		version  string
		url      string
		filename string
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.Contains(line, "AnthropicClaude-") && strings.Contains(line, "-full.nupkg") {
			// Extract version from filename
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				filename := parts[1]
				// Extract version from AnthropicClaude-X.Y.Z-full.nupkg
				versionStart := strings.Index(filename, "AnthropicClaude-") + len("AnthropicClaude-")
				versionEnd := strings.Index(filename, "-full.nupkg")
				if versionStart > 0 && versionEnd > versionStart {
					version := filename[versionStart:versionEnd]
					url := strings.Replace(windowsReleasesURL, "RELEASES", filename, 1)
					versions = append(versions, struct {
						version  string
						url      string
						filename string
					}{version, url, filename})
				}
			}
		}
	}

	if len(versions) == 0 {
		fmt.Printf("ERROR: No valid releases found in RELEASES file\n")
		return "", "", fmt.Errorf("no releases found in Windows RELEASES file")
	}

	// Sort versions to find the latest (newest first)
	sort.Slice(versions, func(i, j int) bool {
		partsI := strings.Split(versions[i].version, ".")
		partsJ := strings.Split(versions[j].version, ".")

		for k := 0; k < len(partsI) && k < len(partsJ); k++ {
			numI, _ := strconv.Atoi(partsI[k])
			numJ, _ := strconv.Atoi(partsJ[k])

			if numI != numJ {
				return numI > numJ
			}
		}

		return len(partsI) > len(partsJ)
	})

	// Use the latest version
	latest := versions[0]
	fmt.Printf("Selected latest version: %s, URL: %s\n", latest.version, latest.url)
	return latest.version, latest.url, nil
}

func downloadAndExtract(version, downloadURL string) error {
	newVersionZipName := fmt.Sprintf("AnthropicClaude-%s-full.nupkg", version)

	// Define the download path based on whether we keep files or use temp
	var newVersionDownloadPath string
	if KeepNupkgFiles {
		newVersionDownloadPath = utils.ResolvePath(newVersionZipName)
	} else {
		newVersionDownloadPath = utils.ResolvePath(newVersionZipName + ".tmp")
	}

	// Check if file already exists when KeepNupkgFiles is enabled
	fileExists := false
	fullPath := utils.ResolvePath(newVersionZipName)
	if _, err := os.Stat(fullPath); err == nil {
		fileExists = true
	}

	if KeepNupkgFiles && fileExists {
		fmt.Printf("Using existing file: %s\n", newVersionZipName)
	} else {
		// Download if file doesn't exist or if we're not keeping files
		fmt.Printf("Downloading from: %s\n", downloadURL)

		resp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("downloading: %v", err)
		}
		defer resp.Body.Close()

		// Use the already defined download path
		outFile, err := os.Create(newVersionDownloadPath)
		if err != nil {
			return fmt.Errorf("creating file: %v", err)
		}
		_, err = io.Copy(outFile, resp.Body)
		outFile.Close()
		if err != nil {
			return fmt.Errorf("saving file: %v", err)
		}
		fmt.Printf("Downloaded: %s\n", newVersionDownloadPath)
	}

	// Extract
	fmt.Println("Extracting...")
	os.RemoveAll(AppFolder)
	os.MkdirAll(AppFolder, 0755)

	zipReader, err := zip.OpenReader(newVersionDownloadPath)
	if err != nil {
		return fmt.Errorf("opening archive: %v", err)
	}
	// Don't defer close - we need to close before deleting temp file

	for _, f := range zipReader.File {
		// Windows - only extract files from lib/net45/
		if !strings.HasPrefix(f.Name, "lib/net45/") {
			continue
		}
		relativePath := strings.TrimPrefix(f.Name, "lib/net45/")

		if relativePath == "" {
			continue
		}

		path := filepath.Join(AppFolder, relativePath)

		// Handle PowerShell Compress-Archive's broken directory entries
		normalizedName := strings.ReplaceAll(f.Name, "\\", "/")
		isDirectory := f.FileInfo().IsDir() || (f.UncompressedSize64 == 0 && (strings.HasSuffix(normalizedName, "/") || strings.HasSuffix(f.Name, "\\")))

		if isDirectory {
			os.MkdirAll(path, 0755)
			continue
		}

		// Skip if path already exists as a directory (created by earlier MkdirAll)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			continue
		}

		os.MkdirAll(filepath.Dir(path), 0755)

		// Regular file extraction
		src, _ := f.Open()
		dst, _ := os.Create(path)
		io.Copy(dst, src)
		dst.Close()
		src.Close()
	}

	// Close the zip reader before attempting to delete temp file
	zipReader.Close()

	// Delete the archive file only if KeepNupkgFiles is false
	if !KeepNupkgFiles {
		os.Remove(newVersionDownloadPath)
	} else {
		fmt.Printf("Keeping archive file: %s\n", newVersionZipName)
	}

	return nil
}
