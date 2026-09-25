package main

import (
	"claude-webext-patcher/patcher"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

// relaunchedEnv marks a launcher started by relaunchInTerminal, so a terminal that
// somehow still gives us no TTY can't cause a relaunch loop.
const relaunchedEnv = "CLAUDE_WEBEXT_IN_TERMINAL"

// prepareAdminContext relaunches the launcher inside a terminal emulator when it was
// started without one (e.g. double-clicked in a file manager), so its output and any
// password prompt are visible, like the console window on Windows and Terminal.app on
// macOS. It then makes sure Chromium's sandbox can start from our install location: on
// Ubuntu 24.04+ that needs a one-time, root-installed AppArmor profile.
func prepareAdminContext() error {
	if !stdinIsTerminal() && os.Getenv(relaunchedEnv) == "" {
		relaunchInTerminal() // only returns if no terminal emulator could be started
	}
	ensureAppArmorProfile()
	return nil
}

// relaunchInTerminal re-runs the launcher inside a terminal emulator and exits. The
// terminal stays open on failure so the error can be read, and closes on success.
// Returns only if no terminal could be started.
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

	for _, t := range candidates {
		bin, err := exec.LookPath(t.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, append(t.args, self...)...)
		cmd.Env = append(os.Environ(), relaunchedEnv+"=1")
		if err := cmd.Start(); err != nil {
			continue
		}
		os.Exit(0)
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
