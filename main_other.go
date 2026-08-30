//go:build !windows

package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// prepareAdminContext relaunches the launcher inside Terminal.app on macOS
// when there is no controlling terminal, so console output is visible.
// On other non-Windows platforms it is a no-op.
func prepareAdminContext() error {
	if runtime.GOOS == "darwin" && os.Getenv("TERM") == "" {
		executable, _ := os.Executable()
		execDir := filepath.Dir(executable)

		// Change to the executable's directory, run, then exit terminal
		// Escape single quotes in paths for AppleScript
		execDirEscaped := strings.ReplaceAll(execDir, `'`, `'\''`)
		executableEscaped := strings.ReplaceAll(executable, `'`, `'\''`)
		script := fmt.Sprintf(`tell application "Terminal"
			set newTab to do script "cd '%s' && '%s' && exit"
			activate
		end tell`, execDirEscaped, executableEscaped)

		cmd := exec.Command("osascript", "-e", script)
		cmd.Start()
		os.Exit(0)
	}
	return nil
}

// releaseAdminContext is a no-op on non-Windows platforms.
func releaseAdminContext() {}

func claudeUserDataDir(instance string) string {
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "Claude-"+instance)
	}
	if runtime.GOOS == "linux" {
		// XDG base directory; fall back to $HOME/.config when unset. The
		// wrapper.js mirrors this exact convention so patched sessions share
		// the same per-instance data dir.
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "Claude-"+instance)
	}
	return ""
}

func claudeExecutablePath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(patcher.AppFolder, "Claude.app", "Contents", "MacOS", "Claude")
	}
	if runtime.GOOS == "linux" {
		// The Linux .deb installs its launcher as claude-desktop.
		return filepath.Join(patcher.AppFolder, "claude-desktop")
	}
	// Other Unix-like systems
	return filepath.Join(patcher.AppFolder, "claude")
}

// claudeInstalled returns true if the Claude executable exists in the install directory.
func claudeInstalled() bool {
	_, err := os.Stat(claudeExecutablePath())
	return err == nil
}

// ensureClaudeReady runs patching and extension updates in-process on macOS.
func ensureClaudeReady(forceUpdate bool) error {
	if err := patcher.EnsurePatched(forceUpdate); err != nil {
		if claudeInstalled() {
			fmt.Printf("Warning: patching failed (%v), launching existing installation.\n", err)
		} else {
			return err
		}
	}
	if err := extensions.UpdateAll(); err != nil {
		fmt.Printf("Warning: extension update failed: %v\n", err)
	}
	if err := patcher.DeploySentinelExtension(); err != nil {
		fmt.Printf("Warning: sentinel extension deployment failed: %v\n", err)
	}
	return nil
}

// runPatcherMode is not used on non-Windows platforms.
func runPatcherMode(forceUpdate bool, debug bool) int {
	fmt.Println("--patcher is not supported on this platform")
	return 1
}

// desktopExecField renders a path for use in a .desktop Exec= line. Per the
// Desktop Entry Specification, arguments containing whitespace (or characters
// that the parser treats specially) must be quoted and escaped.
func desktopExecField(p string) string {
	if strings.ContainsAny(p, " \t\"'\\`$") {
		p = strings.ReplaceAll(p, `\`, `\\`)
		p = strings.ReplaceAll(p, `"`, `\"`)
		return `"` + p + `"`
	}
	return p
}

// desktopEntryContent builds the launcher's .desktop file content for a given
// executable path. It intentionally contains no developer- or machine-specific
// paths — generation happens on the user's machine from their actual path.
func desktopEntryContent(execPath string) string {
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Claude WebExtension Launcher\n" +
		"Comment=Launch a standalone Claude Desktop with web-extension support\n" +
		"Exec=" + desktopExecField(execPath) + "\n" +
		"Terminal=false\n" +
		"Categories=Utility;\n"
}

// ensureDesktopEntry writes ~/.local/share/applications/claude-webext-launcher.desktop
// pointing at the real launcher executable (honouring $XDG_DATA_HOME when set), so a
// freshly-extracted standalone install gains a working desktop entry without manual path
// editing. It is a no-op outside Linux. Rewriting only happens when the content changed,
// so re-running the launcher is idempotent.
func ensureDesktopEntry() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving launcher path for desktop entry: %v", err)
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("resolving launcher path for desktop entry: %v", err)
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return fmt.Errorf("cannot determine XDG_DATA_HOME or home directory for desktop entry")
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(dataHome, "applications")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating applications directory: %v", err)
	}

	entryPath := filepath.Join(dir, "claude-webext-launcher.desktop")
	content := desktopEntryContent(exePath)

	if existing, err := os.ReadFile(entryPath); err == nil && string(existing) == content {
		fmt.Printf("Desktop entry already up to date: %s\n", entryPath)
		return nil
	}

	if err := os.WriteFile(entryPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("writing desktop entry: %v", err)
	}
	fmt.Printf("Desktop entry created: %s\n", entryPath)
	return nil
}

// sandboxDiagnostic returns "" when the given chrome-sandbox helper looks usable,
// otherwise a non-empty guidance string explaining what is wrong and what the user
// can do about it. It never modifies permissions and never invokes sudo.
func sandboxDiagnostic(sandboxPath string) string {
	info, err := os.Stat(sandboxPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "Warning: chrome-sandbox is missing from the standalone install; this usually\n" +
				"  means extraction was incomplete. Re-run the launcher so it rebuilds the install."
		}
		return fmt.Sprintf("Warning: could not inspect chrome-sandbox: %v", err)
	}
	if info.IsDir() {
		return "Warning: chrome-sandbox exists but is a directory rather than the SUID helper."
	}
	if info.Mode()&os.ModeSetuid == 0 {
		return "Warning: chrome-sandbox is present but is not setuid-root, so Electron cannot use\n" +
			"  its SUID sandbox and will rely on unprivileged user namespaces. That is fine on\n" +
			"  most distros; if Claude refuses to start with a sandbox error, you can enable the\n" +
			"  SUID sandbox manually (never done automatically) with:\n" +
			"    sudo chown root:root " + sandboxPath + "\n" +
			"    sudo chmod 4755 " + sandboxPath
	}
	return ""
}

// checkSandbox prints the sandbox diagnostic (Linux only) before Claude is launched.
func checkSandbox() {
	if runtime.GOOS != "linux" {
		return
	}
	if msg := sandboxDiagnostic(filepath.Join(patcher.AppFolder, "chrome-sandbox")); msg != "" {
		fmt.Println(msg)
		fmt.Println("  If Claude still fails to start, run the launcher with --debug to see the error.")
	}
}
