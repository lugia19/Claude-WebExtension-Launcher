//go:build windows

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// migrateCoworkSessions copies the official Claude client's Cowork sessions into the
// default "modified" instance, once. Cowork sessions live in the userData dir under
// local-agent-mode-sessions; the official app uses %APPDATA%\Claude while the patched
// app's default instance uses %APPDATA%\Claude-modified (see wrapper.js). This runs
// unelevated and before the official-uninstall prompt, so the user's existing sessions
// carry over to the patched app. It never overwrites sessions that already exist.
func migrateCoworkSessions() {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return
	}
	src := filepath.Join(appData, "Claude", "local-agent-mode-sessions")
	dst := filepath.Join(appData, "Claude-modified", "local-agent-mode-sessions")

	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return // nothing to migrate
	}
	if _, err := os.Stat(dst); err == nil {
		return // destination already has sessions — don't clobber
	}

	fmt.Println("Migrating Cowork sessions from the official Claude app...")
	if err := copyTree(src, dst); err != nil {
		fmt.Printf("Warning: could not migrate Cowork sessions: %v\n", err)
		return
	}
	fmt.Println("Cowork sessions migrated.")
}

// copyTree recursively copies the directory src to dst.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
}

func checkMSIXAndPrompt(instanceName string) {
	if !isMSIXInstalled() {
		return
	}

	choice := loadMSIXChoice()

	if strings.HasPrefix(choice, "keep:") {
		savedVersion := strings.TrimPrefix(choice, "keep:")
		if savedVersion == Version {
			return
		}
		fmt.Println("Launcher version changed since you last chose to keep the official Claude app.")
	}

	if choice == "uninstall" {
		fmt.Println("Official Claude MSIX was reinstalled since you last removed it.")
	}

	switch promptMSIXChoice() {
	case "uninstall":
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
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("Official Claude Desktop (MSIX) detected.")
	fmt.Println()
	fmt.Println("The official MSIX installation overrides the claude:// protocol")
	fmt.Println("handler, which prevents magic link login from working with the")
	fmt.Println("patched app.")
	fmt.Println()
	fmt.Println("[1] Uninstall the official app (recommended)")
	fmt.Println("[2] Keep it installed (login via magic link won't work)")
	fmt.Println("[3] Ask me later")
	fmt.Println("============================================================")
	fmt.Print("Choose [1/2/3]: ")

	var input string
	fmt.Scanln(&input)
	input = strings.TrimSpace(input)

	switch input {
	case "1":
		return "uninstall"
	case "2":
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
