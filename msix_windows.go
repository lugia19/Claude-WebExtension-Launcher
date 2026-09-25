//go:build windows

package main

import (
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// msixChoiceHandled reports whether a saved decision already covers this launch, so
// no prompt is needed. Only a "keep" for the current launcher version qualifies; a
// stale "keep:" (older version) or an "uninstall" (MSIX reinstalled) still prompts.
func msixChoiceHandled() bool {
	return loadMSIXChoice() == "keep:"+Version
}

func checkMSIXAndPrompt(instanceName string) {
	// Fast path (no lock): nothing installed, or a still-valid decision is saved.
	if !isMSIXInstalled() || msixChoiceHandled() {
		return
	}

	// Serialize with other concurrently-launched instances so only one prompts. The
	// first instance decides and persists it; the rest re-check below and skip. Reuse
	// the patch lock name so this can't interleave with patching.
	lock, locked := utils.AcquirePatchLock(patchLockName, patchLockTimeout)
	if locked {
		defer lock.Release()
	}
	// If we couldn't get the lock in time, fall through — the re-check below usually
	// short-circuits anyway (decision saved / MSIX already removed by another instance).

	// Re-evaluate under the lock: another instance may have just handled it.
	if !isMSIXInstalled() || msixChoiceHandled() {
		return
	}

	choice := loadMSIXChoice()
	if strings.HasPrefix(choice, "keep:") {
		fmt.Println("Launcher version changed since you last chose to keep the official Claude app.")
	} else if choice == "uninstall" {
		fmt.Println("Official Claude MSIX was reinstalled since you last removed it.")
	}

	switch promptMSIXChoice() {
	case "uninstall":
		// Drop the official install's session junctions first so the uninstaller can't
		// follow them into the shared store and delete the sessions.
		CleanupOfficialJunctions()
		if err := uninstallMSIX(); err != nil {
			fmt.Printf("Failed to uninstall MSIX: %v\n", err)
			fmt.Println("Continuing without removing it. You can try again next launch.")
			return
		}
		saveMSIXChoice("uninstall")
		fmt.Println("Official Claude app removed successfully.")
	case "keep":
		saveMSIXChoice("keep:" + Version)
		fmt.Println("Keeping official Claude app. Magic link login will not work with the patched app.")
	default:
		fmt.Println("Skipping for now. You'll be asked again next launch.")
	}
}

func isMSIXInstalled() bool {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-AppxPackage -Name 'Claude' -ErrorAction SilentlyContinue").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func promptMSIXChoice() string {
	choice := ui.Ask(
		"Official Claude Desktop (MSIX) detected.",
		"It takes over claude:// links, so magic-link login won't reach the patched app.",
		[]string{"Uninstall it (recommended)", "Keep it (login codes only)", "Ask me later"},
	)
	switch choice {
	case 0:
		return "uninstall"
	case 1:
		return "keep"
	default:
		return "ask-later"
	}
}

func uninstallMSIX() error {
	fmt.Println("Stopping official Claude process...")
	exec.Command("powershell", "-NoProfile", "-Command",
		"Get-Process -Name 'Claude' -ErrorAction SilentlyContinue | "+
			"Where-Object { $_.Path -and $_.Path -notlike '*ClaudeWebExtLauncher*' } | "+
			"Stop-Process -Force").Run()

	fmt.Println("Removing MSIX package...")
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-AppxPackage -Name 'Claude' | Remove-AppxPackage").CombinedOutput()
	if err != nil {
		return fmt.Errorf("Remove-AppxPackage failed: %v\n%s", err, out)
	}

	if isMSIXInstalled() {
		return fmt.Errorf("package still present after removal")
	}

	return nil
}

func msixChoicePath() string {
	return filepath.Join(os.Getenv("APPDATA"), "ClaudeWebExtLauncher", "msix-choice.txt")
}

func loadMSIXChoice() string {
	data, err := os.ReadFile(msixChoicePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func saveMSIXChoice(choice string) {
	p := msixChoicePath()
	os.MkdirAll(filepath.Dir(p), 0755)
	os.WriteFile(p, []byte(choice), 0644)
}
