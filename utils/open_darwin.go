package utils

import (
	"os/exec"
	"strings"
)

// OpenInViewer opens path in its default application.
func OpenInViewer(path string) error {
	return exec.Command("open", path).Start()
}

// ShowErrorDialog shows a native error dialog. Used when the status window couldn't
// open.
func ShowErrorDialog(title, message string) {
	quote := func(s string) string {
		return `"` + strings.NewReplacer(`\`, `\`, `"`, `\"`).Replace(s) + `"`
	}
	script := "display dialog " + quote(message) + " with title " + quote(title) +
		` buttons {"OK"} default button "OK" with icon stop`
	exec.Command("osascript", "-e", script).Run()
}
