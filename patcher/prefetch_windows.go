//go:build windows

package patcher

import (
	"claude-webext-patcher/utils"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The Claude MSIX is downloaded by the unelevated launcher (so the window can show
// progress) and handed to the elevated worker by path. That path is user-writable, so
// the worker never trusts it: it copies the package into the admin-only install
// folder and verifies Anthropic's signature on that copy before extracting anything.
// See useVerifiedPackage.

// msixSigner is the publisher Claude's MSIX must be signed by. Matched on the subject
// rather than a thumbprint, which changes whenever Anthropic renews the certificate.
const msixSigner = `CN="Anthropic, PBC", O="Anthropic, PBC"`

// Prefetch downloads the Claude MSIX (unelevated) into the per-user download cache
// and returns its path.
func Prefetch(version, url string) (string, error) {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "ClaudeWebExtLauncher", "downloads")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("Claude-%s.msix", version))
	if err := downloadFile(url, path); err != nil {
		return "", err
	}
	return path, nil
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
	cmd := utils.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
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
