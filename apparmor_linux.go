package main

import (
	"claude-webext-patcher/utils"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Ubuntu 24.04+ sets kernel.apparmor_restrict_unprivileged_userns=1, which stops
// unconfined programs from using user namespaces — and with them Chromium's sandbox, so
// Claude refuses to start. The official .deb installs an AppArmor profile that
// allowlists /usr/lib/claude-desktop/claude-desktop for userns; our copy lives
// elsewhere, so it needs a profile of its own. Like the official one it is
// flags=(unconfined): it doesn't confine Claude, it only permits userns.
//
// The profile is keyed on the install path, which stays the same across updates
// (app-latest is swapped in place), so this is a one-time root operation.

const (
	appArmorRestrictSysctl = "/proc/sys/kernel/apparmor_restrict_unprivileged_userns"
	appArmorDir            = "/etc/apparmor.d"
)

// appArmorProfile returns our profile's path and expected content, or ok=false when
// this system doesn't need one.
func appArmorProfile() (path, content string, ok bool) {
	if data, err := os.ReadFile(appArmorRestrictSysctl); err != nil || strings.TrimSpace(string(data)) != "1" {
		return "", "", false // no userns restriction; the sandbox works without a profile
	}
	// Same gate as the official postinst: the profile uses abi/4.0 syntax, which
	// AppArmor 3.x can't parse (and 3.x has no userns restriction anyway).
	if _, err := os.Stat(filepath.Join(appArmorDir, "abi", "4.0")); err != nil {
		return "", "", false
	}

	// One profile per user, since each user's install path differs.
	name := fmt.Sprintf("claude-webext-launcher-%d", os.Getuid())
	exePath := filepath.Join(utils.ResolvePath("app-latest"), "claude-desktop")
	if strings.ContainsAny(exePath, "\"\n") {
		fmt.Printf("Warning: cannot write an AppArmor profile for %q (unsupported characters in path).\n", exePath)
		return "", "", false
	}
	content = fmt.Sprintf(`abi <abi/4.0>,
include <tunables/global>

profile %s "%s" flags=(unconfined) {
  userns,
}
`, name, exePath)
	return filepath.Join(appArmorDir, name), content, true
}

// sandboxNeeded reports whether the AppArmor profile has to be (re)installed.
func sandboxNeeded() bool {
	path, content, ok := appArmorProfile()
	if !ok {
		return false
	}
	existing, err := os.ReadFile(path)
	return err != nil || string(existing) != content
}

// installSandbox installs the AppArmor profile through pkexec, which shows the
// desktop's own password dialog. On failure the manual steps go to the log.
func installSandbox() error {
	path, content, ok := appArmorProfile()
	if !ok {
		return nil
	}
	fmt.Println("This system restricts user namespaces (Ubuntu 24.04+), which Claude's sandbox needs.")
	fmt.Printf("Installing AppArmor profile %s (one-time, needs your password)...\n", path)

	err := writeAppArmorProfile(path, content)
	if err == nil {
		fmt.Println("AppArmor profile installed.")
		return nil
	}
	fmt.Printf("Could not install the AppArmor profile: %v\n", err)
	fmt.Println("Claude will likely fail to start with a sandbox error until it's installed. To install it manually, run:")
	fmt.Printf("\n  sudo tee %s > /dev/null <<'EOF'\n%sEOF\n", path, content)
	fmt.Printf("  sudo apparmor_parser -r -W -T %s\n\n", path)
	return err
}

func writeAppArmorProfile(path, content string) error {
	if _, err := exec.LookPath("pkexec"); err != nil {
		return errors.New("pkexec is not installed")
	}

	tmp, err := os.CreateTemp("", "claude-webext-apparmor-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(content)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	os.Chmod(tmp.Name(), 0644)

	// Positional args keep the paths out of the shell string entirely. If the parser
	// rejects the profile, remove the file again: an unloaded profile left on disk
	// would match on the next launch and never be retried.
	script := `install -m 0644 "$1" "$2" && { apparmor_parser -r -W -T "$2" || { rm -f "$2"; exit 1; }; }`
	cmd := exec.Command("pkexec", "/bin/sh", "-c", script, "sh", tmp.Name(), path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// pkexec exits 126 when the password dialog is dismissed.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 126 {
			return errors.New("password prompt was cancelled")
		}
		return fmt.Errorf("pkexec: %v", err)
	}
	return nil
}
