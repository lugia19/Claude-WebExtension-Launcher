package main

import (
	"claude-webext-patcher/patcher"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// relaunchedEnv marks a launcher started by relaunchInTerminal (so a terminal that
// somehow still gives us no TTY can't cause a relaunch loop). Its value is a marker
// file the relaunched copy creates to confirm it actually started.
const relaunchedEnv = "CLAUDE_WEBEXT_IN_TERMINAL"

// relaunchConfirmTimeout is how long to wait for the relaunched copy to check in. A
// terminal binary can start fine and still fail to open a window (no display, no
// session bus), so starting it isn't proof enough to exit.
const relaunchConfirmTimeout = 10 * time.Second

// prepareAdminContext relaunches the launcher inside a terminal emulator when it was
// started without one (e.g. double-clicked in a file manager), so its output and any
// password prompt are visible, like the console window on Windows and Terminal.app on
// macOS. It then makes sure Chromium's sandbox can start from our install location: on
// Ubuntu 24.04+ that needs a one-time, root-installed AppArmor profile.
func prepareAdminContext() error {
	if marker := os.Getenv(relaunchedEnv); marker != "" {
		// We are the relaunched copy: tell the original we made it into a terminal.
		os.WriteFile(marker, nil, 0600)
	} else if !guiMode && !stdinIsTerminal() {
		relaunchInTerminal() // only returns if no terminal could be started
	}
	ensureAppArmorProfile()
	return nil
}

// relaunchInTerminal re-runs the launcher inside a terminal emulator and exits once the
// new copy confirms it started. The terminal stays open on failure so the error can be
// read, and closes on success. Returns only if no terminal worked, in which case the
// caller simply carries on without one.
func relaunchInTerminal() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	script := `"$0" "$@" || { echo; printf 'Press Enter to close...'; read _; }`
	self := append([]string{"/bin/sh", "-c", script, exe}, os.Args[1:]...)

	type terminal struct {
		bin  string
		args []string // placed before the command
	}
	var candidates []terminal
	if t := os.Getenv("TERMINAL"); t != "" {
		candidates = append(candidates, terminal{t, []string{"-e"}})
	}
	// Native "--" terminals go before x-terminal-emulator: on Ubuntu that's a wrapper
	// around gnome-terminal whose -e argument handling is unreliable.
	candidates = append(candidates,
		terminal{"gnome-terminal", []string{"--"}},
		terminal{"ptyxis", []string{"--"}},
		terminal{"konsole", []string{"-e"}},
		terminal{"x-terminal-emulator", []string{"-e"}},
		terminal{"xfce4-terminal", []string{"-x"}},
		terminal{"kitty", nil},
		terminal{"alacritty", []string{"-e"}},
		terminal{"xterm", []string{"-e"}},
	)

	marker := filepath.Join(os.TempDir(), fmt.Sprintf("claude-webext-relaunch-%d", os.Getpid()))
	defer os.Remove(marker)

	for _, t := range candidates {
		bin, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		os.Remove(marker)
		cmd := exec.Command(bin, append(t.args, self...)...)
		cmd.Env = append(os.Environ(), relaunchedEnv+"="+marker)
		if err := cmd.Start(); err != nil {
			continue
		}
		go cmd.Wait() // reap it; many terminals hand off to a server and exit at once

		for deadline := time.Now().Add(relaunchConfirmTimeout); time.Now().Before(deadline); {
			if _, err := os.Stat(marker); err == nil {
				os.Remove(marker)
				os.Exit(0)
			}
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Printf("%s did not start the launcher, trying the next terminal...\n", t.bin)
	}
}

// stdinIsTerminal reports whether stdin is a TTY. A desktop launch usually gets
// /dev/null, which is also a character device, so this asks the tty layer directly
// instead of checking the file mode.
func stdinIsTerminal() bool {
	var termios syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdin.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&termios)))
	return errno == 0
}

// claudeUserDataDir mirrors Electron's appData on Linux: $XDG_CONFIG_HOME, defaulting
// to ~/.config. The wrapper appends "-<instance>" to the app name there.
func claudeUserDataDir(instance string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "Claude-"+instance)
}

func claudeExecutablePath() string {
	return filepath.Join(patcher.AppFolder, "claude-desktop")
}

// detachFromTerminal starts Claude in its own session, so closing the terminal the
// launcher ran in (which SIGHUPs its process group) doesn't take Claude down with it.
func detachFromTerminal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
