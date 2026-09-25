//go:build windows

package patcher

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The Claude MSIX is downloaded by the unelevated launcher (so the status window can
// show progress) and handed to the elevated patcher by path. That path is
// user-writable, so the elevated side never trusts it: it copies the package into the
// admin-only install folder and verifies Anthropic's signature on that copy before
// extracting anything. See useVerifiedPackage.

// msixSigner is the publisher Claude's MSIX must be signed by. Matched on the subject
// rather than a thumbprint, which changes whenever Anthropic renews the certificate.
const msixSigner = `CN="Anthropic, PBC", O="Anthropic, PBC"`

// PrefetchWindowsPackage downloads the Claude MSIX without elevation when the
// elevated patcher is going to need it, mirroring EnsurePatched's decision (a new
// version, a changed patch version, or a forced update). Returns the file and its
// version, or empty strings when no download is needed.
func PrefetchWindowsPackage(forceUpdate bool) (path, version string, err error) {
	current := readTrimmed(filepath.Join(installBaseDir, "claude-version.txt"))
	patch := readTrimmed(filepath.Join(installBaseDir, "patch-version.txt"))

	latest, url, err := GetLatestVersion()
	if err != nil {
		return "", "", err
	}
	if !forceUpdate && current == latest && patch == PatchVersion {
		return "", "", nil // only extensions/Cowork need admin work; nothing to download
	}

	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "ClaudeWebExtLauncher", "downloads")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	path = filepath.Join(dir, fmt.Sprintf("Claude-%s.msix", latest))

	fmt.Printf("Downloading from: %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("downloading: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("downloading: HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(path)
	if err != nil {
		return "", "", err
	}
	_, err = io.Copy(out, progressBody(resp))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", "", fmt.Errorf("saving download: %v", err)
	}
	fmt.Printf("Downloaded: %s\n", path)
	return path, latest, nil
}

// useVerifiedPackage copies the prefetched MSIX for version into dst (inside the
// admin-only install folder) and verifies its signature there. Returns false (after
// logging why) if there is no usable prefetched package, so the caller downloads it
// itself.
func useVerifiedPackage(version, dst string) bool {
	if PrefetchedPackage == "" {
		return false
	}
	if PrefetchedVersion != version {
		fmt.Printf("Prefetched package is %s but installing %s; downloading instead.\n", PrefetchedVersion, version)
		return false
	}
	if err := copyFileTo(PrefetchedPackage, dst); err != nil {
		fmt.Printf("Could not use prefetched package (%v); downloading instead.\n", err)
		return false
	}
	if err := verifyMSIXSignature(dst); err != nil {
		os.Remove(dst)
		fmt.Printf("Prefetched package failed verification (%v); downloading instead.\n", err)
		return false
	}
	fmt.Println("Using the package downloaded by the launcher (signature verified).")
	return true
}

// verifyMSIXSignature checks the package's Authenticode signature is valid and from
// Anthropic. A single flipped byte anywhere in the package reports HashMismatch.
func verifyMSIXSignature(path string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		`$s = Get-AuthenticodeSignature -LiteralPath $env:CWL_MSIX; "$($s.Status)"; "$($s.SignerCertificate.Subject)"`)
	cmd.Env = append(os.Environ(), "CWL_MSIX="+path)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("running Get-AuthenticodeSignature: %v", err)
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	status := strings.TrimSpace(lines[0])
	subject := ""
	if len(lines) > 1 {
		subject = strings.TrimSpace(lines[1])
	}
	if status != "Valid" {
		return fmt.Errorf("signature status %s", status)
	}
	if !strings.HasPrefix(subject, msixSigner) {
		return fmt.Errorf("signed by %q, expected Anthropic", subject)
	}
	return nil
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

func readTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
