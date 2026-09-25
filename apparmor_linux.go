package main

import (
	"bufio"
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

func ensureAppArmorProfile() {
	if data, err := os.ReadFile(appArmorRestrictSysctl); err != nil || strings.TrimSpace(string(data)) != "1" {
		return // no userns restriction; the sandbox works without a profile
	}
	// Same gate as the official postinst: the profile uses abi/4.0 syntax, which
	// AppArmor 3.x can't parse (and 3.x has no userns restriction anyway).
	if _, err := os.Stat(filepath.Join(appArmorDir, "abi", "4.0")); err != nil {
		return
	}

	// One profile per user, since each user's install path differs.
	name := fmt.Sprintf("claude-webext-launcher-%d", os.Getuid())
	profilePath := filepath.Join(appArmorDir, name)
	exePath := filepath.Join(utils.ResolvePath("app-latest"), "claude-desktop")
	if strings.ContainsAny(exePath, "\"\n") {
		fmt.Printf("Warning: cannot write an AppArmor profile for %q (unsupported characters in path).\n", exePath)
		return
	}
	profile := fmt.Sprintf(`abi <abi/4.0>,
include <tunables/global>

profile %s "%s" flags=(unconfined) {
  userns,
}
`, name, exePath)

	if existing, err := os.ReadFile(profilePath); err == nil && string(existing) == profile {
		return
	}

	fmt.Println("This system restricts user namespaces (Ubuntu 24.04+), which Claude's sandbox needs.")
	fmt.Printf("Installing an AppArmor profile for %s (one-time, needs your password)...\n", exePath)

	tmp, err := os.CreateTemp("", "claude-webext-apparmor-*")
	if err != nil {
		printManualAppArmorSteps(profilePath, profile, err)
		return
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(profile)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		printManualAppArmorSteps(profilePath, profile, err)
		return
	}
	os.Chmod(tmp.Name(), 0644)

	// Positional args keep the paths out of the shell string entirely. If the parser
	// rejects the profile, remove the file again: an unloaded profile left on disk
	// would match on the next launch and never be retried.
	script := `install -m 0644 "$1" "$2" && { apparmor_parser -r -W -T "$2" || { rm -f "$2"; exit 1; }; }`
	shArgs := []string{"/bin/sh", "-c", script, "sh", tmp.Name(), profilePath}

	var lastErr error
	userDismissed := false
	if _, err := exec.LookPath("pkexec"); err == nil {
		if lastErr = runElevated("pkexec", shArgs); lastErr == nil {
			fmt.Println("AppArmor profile installed.")
			return
		}
		// pkexec exits 126 when the user dismisses the password dialog; anything else
		// (e.g. 127, no polkit agent running) is worth retrying through sudo.
		var exitErr *exec.ExitError
		userDismissed = errors.As(lastErr, &exitErr) && exitErr.ExitCode() == 126
	}
	// The launcher relaunches itself in a terminal when started without one (see
	// prepareAdminContext), so sudo can normally prompt; stdin is only missing a TTY
	// when no terminal emulator could be found.
	if !userDismissed {
		if stdinIsTerminal() {
			if lastErr = runElevated("sudo", shArgs); lastErr == nil {
				fmt.Println("AppArmor profile installed.")
				return
			}
		} else if lastErr == nil {
			lastErr = fmt.Errorf("no pkexec, and no terminal to run sudo in")
		}
	}

	printManualAppArmorSteps(profilePath, profile, lastErr)
}

func runElevated(tool string, args []string) error {
	cmd := exec.Command(tool, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", tool, err)
	}
	return nil
}

func printManualAppArmorSteps(profilePath, profile string, cause error) {
	fmt.Printf("\nWarning: could not install the AppArmor profile (%v).\n", cause)
	fmt.Println("Claude will likely fail to start with a sandbox error until it's installed.")
	fmt.Println("To install it manually, run:")
	fmt.Printf("\n  sudo tee %s > /dev/null <<'EOF'\n%sEOF\n", profilePath, profile)
	fmt.Printf("  sudo apparmor_parser -r -W -T %s\n\n", profilePath)
	if stdinIsTerminal() {
		fmt.Print("Press Enter to continue...")
		bufio.NewReader(os.Stdin).ReadString('\n')
	}
}
