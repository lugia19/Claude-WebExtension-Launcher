//go:build windows

package patcher

import (
	"archive/zip"
	"claude-webext-patcher/utils"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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

// deployDLL extracts the embedded version.dll matching the host architecture next to
// claude.exe. The proxy DLL is loaded by claude.exe via DLL sideloading, so it must match
// the architecture of the (native) claude.exe we installed — x64 or arm64.
func deployDLL() error {
	arch := HostArch()
	srcName := fmt.Sprintf("resources/version-%s.dll", arch)
	dllData, err := EmbeddedFS.ReadFile(srcName)
	if err != nil {
		return fmt.Errorf("reading embedded %s: %v", srcName, err)
	}

	// Guard against a wrong-arch or placeholder DLL: the bundled bytes must be a PE for the
	// expected machine. On arm64 builds without a real version-arm64.dll this fails loudly
	// instead of deploying a DLL that claude.exe can't load.
	if err := verifyDLLArch(dllData, arch); err != nil {
		return err
	}

	dllPath := filepath.Join(AppFolder, "version.dll")
	if err := os.WriteFile(dllPath, dllData, 0755); err != nil {
		return fmt.Errorf("writing version.dll: %v", err)
	}

	fmt.Printf("Deployed version.dll (%s)\n", arch)
	return nil
}

// peMachine returns the PE/COFF Machine field of a Windows binary, or 0 if the bytes are not
// a recognizable PE image.
func peMachine(data []byte) uint16 {
	// DOS header: "MZ", e_lfanew (LE uint32) at offset 0x3C points to the PE header.
	if len(data) < 0x40 || data[0] != 'M' || data[1] != 'Z' {
		return 0
	}
	peOff := int(data[0x3C]) | int(data[0x3D])<<8 | int(data[0x3E])<<16 | int(data[0x3F])<<24
	// PE signature "PE\0\0" then COFF header; Machine is the first COFF field.
	if peOff < 0 || peOff+6 > len(data) {
		return 0
	}
	if data[peOff] != 'P' || data[peOff+1] != 'E' || data[peOff+2] != 0 || data[peOff+3] != 0 {
		return 0
	}
	return uint16(data[peOff+4]) | uint16(data[peOff+5])<<8
}

// verifyDLLArch confirms the bundled DLL is a PE for the expected architecture.
func verifyDLLArch(data []byte, arch string) error {
	want := uint16(0x8664) // IMAGE_FILE_MACHINE_AMD64
	if arch == "arm64" {
		want = imageFileMachineARM64
	}
	got := peMachine(data)
	if got != want {
		return fmt.Errorf("bundled version-%s.dll is not a valid %s DLL (PE machine 0x%04X, want 0x%04X) — "+
			"build the %s Release of ClaudeDLL and replace resources/version-%s.dll", arch, arch, got, want, arch, arch)
	}
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
	arch := HostArch()
	fmt.Printf("Getting latest version for OS: windows (%s)\n", arch)
	if arch == "arm64" {
		fmt.Println("Detected ARM64 host — installing native arm64 Claude.")
	}
	redirectURL := fmt.Sprintf(windowsMSIXRedirectURLFmt, arch)

	// Resolve the MSIX redirect without following it — the 307 response carries the
	// real download URL in its Location header, and we avoid pulling the ~222 MB body.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(redirectURL)
	if err != nil {
		return "", "", fmt.Errorf("resolving MSIX redirect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", "", fmt.Errorf("unexpected status from MSIX redirect endpoint: %d", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", "", fmt.Errorf("MSIX redirect endpoint returned no Location header")
	}

	version, err := parseVersionFromMSIXURL(loc)
	if err != nil {
		return "", "", err
	}

	fmt.Printf("Selected latest version: %s, URL: %s\n", version, loc)
	return version, loc, nil
}

// parseVersionFromMSIXURL extracts the version segment from a Claude MSIX download URL of the
// form https://downloads.claude.ai/releases/win32/x64/{VERSION}/Claude-{hash}.msix.
func parseVersionFromMSIXURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing MSIX URL %q: %v", rawURL, err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("cannot parse version from MSIX URL path: %s", u.Path)
	}
	// The version is the path segment just before the filename.
	version := parts[len(parts)-2]
	// Defensive: a version looks like a dotted number (e.g. 1.11847.5).
	if version == "" || !strings.Contains(version, ".") {
		return "", fmt.Errorf("unexpected version segment %q in MSIX URL: %s", version, u.Path)
	}
	return version, nil
}

func downloadAndExtract(version, downloadURL string) error {
	newVersionZipName := fmt.Sprintf("Claude-%s.msix", version)

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
		// Windows - the MSIX wraps the whole app under app/; everything else
		// (AppxManifest.xml, AppxBlockMap.xml, Assets/, signature, etc.) is skipped.
		if !strings.HasPrefix(f.Name, "app/") {
			continue
		}
		relativePath := strings.TrimPrefix(f.Name, "app/")

		if relativePath == "" {
			continue
		}

		// MSIX part names follow OPC, which percent-encodes reserved characters
		// (e.g. "@" -> "%40"). Decode so files land on disk with their real names —
		// app.asar's unpacked entries (e.g. node_modules/@ant/...) reference the
		// decoded form, and the asar repack copies them by that name.
		if decoded, err := url.PathUnescape(relativePath); err == nil {
			relativePath = decoded
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
