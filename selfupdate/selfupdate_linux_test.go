//go:build linux

package selfupdate

import (
	"strings"
	"testing"
)

func sampleLinuxAssets() []releaseAsset {
	return []releaseAsset{
		{Name: "Claude_WebExtension_Launcher-3.3.3-macos-amd64.zip", DownloadURL: "https://example/macos-amd64"},
		{Name: "Claude_WebExtension_Launcher-3.3.3-macos-arm64.zip", DownloadURL: "https://example/macos-arm64"},
		{Name: "Claude_WebExtension_Launcher-3.3.3-windows.zip", DownloadURL: "https://example/windows"},
		{Name: "Claude_WebExtension_Launcher-3.3.3-linux-amd64.zip", DownloadURL: "https://example/linux-amd64"},
		{Name: "Claude_WebExtension_Launcher-3.3.3-linux-arm64.zip", DownloadURL: "https://example/linux-arm64"},
	}
}

// TestLinuxSelectAssetAMD64 confirms an amd64 host gets the amd64 asset and
// never the arm64 one.
func TestLinuxSelectAssetAMD64(t *testing.T) {
	url, name, err := linuxSelectAsset(sampleLinuxAssets(), "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Claude_WebExtension_Launcher-3.3.3-linux-amd64.zip" {
		t.Fatalf("expected amd64 asset, got %s", name)
	}
	if strings.Contains(name, "arm64") {
		t.Fatalf("amd64 host must never match an arm64 asset, got %s", name)
	}
	if url != "https://example/linux-amd64" {
		t.Fatalf("unexpected URL: %s", url)
	}
}

// TestLinuxSelectAssetARM64 confirms an arm64 host gets the arm64 asset and
// never the amd64 one.
func TestLinuxSelectAssetARM64(t *testing.T) {
	_, name, err := linuxSelectAsset(sampleLinuxAssets(), "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Claude_WebExtension_Launcher-3.3.3-linux-arm64.zip" {
		t.Fatalf("expected arm64 asset, got %s", name)
	}
	if strings.Contains(name, "amd64") {
		t.Fatalf("arm64 host must never match an amd64 asset, got %s", name)
	}
}

// TestLinuxSelectAssetNoArchMatch falls back to a generic -linux zip without
// arch-matching (a legacy single-arch release).
func TestLinuxSelectAssetNoArchMatch(t *testing.T) {
	assets := []releaseAsset{
		{Name: "Claude_WebExtension_Launcher-3.2.0-linux.zip", DownloadURL: "https://example/linux"},
		{Name: "Claude_WebExtension_Launcher-3.2.0-macos.zip", DownloadURL: "https://example/macos"},
	}
	url, name, err := linuxSelectAsset(assets, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if name != "Claude_WebExtension_Launcher-3.2.0-linux.zip" {
		t.Fatalf("expected generic linux fallback, got %s", name)
	}
	if url != "https://example/linux" {
		t.Fatalf("unexpected URL: %s", url)
	}
}

// TestLinuxSelectAssetNoLinuxRelease returns a clear error when no Linux asset
// exists, so a Linux user never reaches any macOS download/install logic.
func TestLinuxSelectAssetNoLinuxRelease(t *testing.T) {
	assets := []releaseAsset{
		{Name: "Claude_WebExtension_Launcher-3.3.3-macos-amd64.zip"},
		{Name: "Claude_WebExtension_Launcher-3.3.3-windows.zip"},
	}
	_, _, err := linuxSelectAsset(assets, "amd64")
	if err == nil {
		t.Fatal("expected error when no linux asset exists")
	}
	if !strings.Contains(err.Error(), "no compatible release file found for linux") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
