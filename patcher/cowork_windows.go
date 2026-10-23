//go:build windows

package patcher

import (
	"claude-webext-patcher/utils"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Older launcher versions registered Cowork's sandbox service, CoworkVMService (a
// LocalSystem service running cowork-svc.exe from our install, which the official
// MSIX declares in its AppxManifest), plus firewall rules for it. Claude has since
// turned local Cowork off, so the launcher no longer sets these up and only removes
// them: on uninstall, and in the worker, where the running service would keep
// app-latest from being removed.
const (
	coworkServiceName = "CoworkVMService"
	coworkFirewallIn  = "ClaudeWebExtLauncher-Cowork-In"
	coworkFirewallOut = "ClaudeWebExtLauncher-Cowork-Out"
)

// RemoveCoworkService stops and deletes CoworkVMService only if it's ours (its binary
// is in our install; an official one is left alone), and removes our firewall rules
// either way. Must be called elevated, before the install folder is deleted
// (cowork-svc.exe is locked while it runs).
func RemoveCoworkService() {
	out, qcErr := runSC("qc", coworkServiceName)
	if strings.Contains(strings.ToLower(out), strings.ToLower(filepath.Clean(utils.WindowsInstallDir))) {
		fmt.Println("Removing CoworkVMService...")
		runSC("stop", coworkServiceName)
		for i := 0; i < 50; i++ { // up to ~10 s for it to stop
			if state, _ := runSC("query", coworkServiceName); !strings.Contains(state, "RUNNING") && !strings.Contains(state, "STOP_PENDING") {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if out, err := runSC("delete", coworkServiceName); err != nil {
			fmt.Printf("Warning: deleting CoworkVMService failed: %v\n%s\n", err, out)
		}
	} else if qcErr == nil {
		fmt.Println("CoworkVMService isn't ours (an official install's); leaving it.")
	}
	deleteCoworkFirewallRules()
}

// CoworkFirewallRulesExist reports whether our Cowork firewall rules are there.
func CoworkFirewallRulesExist() bool {
	return utils.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+coworkFirewallIn).Run() == nil
}

// deleteCoworkFirewallRules removes our Cowork firewall rules ("no rules match" is fine).
func deleteCoworkFirewallRules() {
	utils.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+coworkFirewallIn).Run()
	utils.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+coworkFirewallOut).Run()
}

// runSC runs an sc.exe command and returns its combined output.
func runSC(args ...string) (string, error) {
	out, err := utils.Command("sc.exe", args...).CombinedOutput()
	return string(out), err
}
