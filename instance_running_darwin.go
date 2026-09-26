package main

import (
	"os/exec"
	"strings"
)

// instanceRunning reports whether an instance of Claude is open. On macOS Electron
// leaves no single-instance lock in the data folder, so this looks for the patched
// Claude's main process started for that instance instead (the helper processes are
// other executables). Names compare case-insensitively, like the file system that
// holds their data.
func instanceRunning(instance string) bool {
	out, err := exec.Command("ps", "-axww", "-o", "args=").Output()
	if err != nil {
		return false
	}
	exe := claudeExecutablePath()
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != exe && !strings.HasPrefix(line, exe+" ") {
			continue
		}
		if name, ok := instanceArg(strings.Fields(line[len(exe):])); ok {
			if strings.EqualFold(name, instance) {
				return true
			}
		} else if strings.EqualFold(instance, mainInstanceName) || strings.EqualFold(instance, legacyMainInstanceName) {
			return true // no --instance: the main one (wrapper.js)
		}
	}
	return false
}

// instanceArg finds the --instance value in Claude's arguments, in either form
// wrapper.js accepts: --instance=name or --instance name.
func instanceArg(args []string) (string, bool) {
	for i, a := range args {
		if name, ok := strings.CutPrefix(a, "--instance="); ok {
			return name, true
		}
		if a == "--instance" && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}
