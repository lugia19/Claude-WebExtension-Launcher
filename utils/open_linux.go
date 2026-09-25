package utils

import "os/exec"

// OpenInViewer opens path in its default application.
func OpenInViewer(path string) error {
	return exec.Command("xdg-open", path).Start()
}

// ShowErrorDialog shows a native error dialog if zenity or kdialog is available (the
// log still has the details otherwise). Used when the status window couldn't open.
func ShowErrorDialog(title, message string) {
	if _, err := exec.LookPath("zenity"); err == nil {
		exec.Command("zenity", "--error", "--title="+title, "--text="+message, "--no-markup").Run()
		return
	}
	if _, err := exec.LookPath("kdialog"); err == nil {
		exec.Command("kdialog", "--title", title, "--error", message).Run()
	}
}
