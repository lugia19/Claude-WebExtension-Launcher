//go:build windows

package patcher

import (
	"claude-webext-patcher/utils"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Cowork's sandbox is provided by CoworkVMService, a LocalSystem service running
// cowork-svc.exe (it boots a lightweight Hyper-V/HCS VM). The official MSIX declares
// this service in its AppxManifest and Windows registers it at install time. Since we
// install standalone (bypassing the MSIX), we replicate the registration ourselves as a
// plain WIN32_OWN_PROCESS service. cowork-svc.exe uses standard service-control APIs and
// queries no package identity, so a non-packaged service is sufficient.
const (
	coworkServiceName = "CoworkVMService"
	coworkPipeName    = "cowork-vm-service" // service starts on access to \pipe\cowork-vm-service
	coworkFirewallIn  = "ClaudeWebExtLauncher-Cowork-In"
	coworkFirewallOut = "ClaudeWebExtLauncher-Cowork-Out"
)

// coworkSvcExePath returns the install path of the bundled cowork-svc.exe.
func coworkSvcExePath() string {
	return filepath.Join(appResourcesDir, "cowork-svc.exe")
}

// CoworkServiceExists reports whether the CoworkVMService is registered. Querying a
// service does not require administrator privileges, so this is safe to call from the
// unelevated launcher (e.g. checkNeedsAdmin).
func CoworkServiceExists() bool {
	return utils.Command("sc.exe", "query", coworkServiceName).Run() == nil
}

// RegisterCoworkService registers CoworkVMService if it is not already present.
//
// "Create only if absent" keeps this idempotent and means we never disturb a service we
// don't own: if a foreign CoworkVMService already exists (e.g. from an official install),
// we leave it alone. Must be called from the elevated patcher.
func RegisterCoworkService() error {
	if CoworkServiceExists() {
		fmt.Println("CoworkVMService already registered, leaving it as-is.")
		return nil
	}

	binPath := coworkSvcExePath()
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("cowork-svc.exe not found at %s: %v", binPath, err)
	}

	fmt.Println("Registering CoworkVMService...")

	// Mirror the official service config: WIN32_OWN_PROCESS, auto-start, LocalSystem,
	// ErrorControl=ignore, DisplayName "Claude". Each "key=" token and its value are passed
	// as separate args so the command line ends up as `binPath= "<path>" start= auto ...`,
	// which is the syntax sc.exe expects.
	if out, err := runSC(
		"create", coworkServiceName,
		"binPath=", binPath,
		"start=", "auto",
		"type=", "own",
		"error=", "ignore",
		"obj=", "LocalSystem",
		"DisplayName=", "Claude",
	); err != nil {
		return fmt.Errorf("creating service: %v\n%s", err, out)
	}

	// Best-effort metadata/config that matches the official packaged service.
	if out, err := runSC("description", coworkServiceName, "Desktop application for Claude.ai"); err != nil {
		fmt.Printf("Warning: setting service description failed: %v\n%s\n", err, out)
	}
	if out, err := runSC("sidtype", coworkServiceName, "unrestricted"); err != nil {
		fmt.Printf("Warning: setting service SID type failed: %v\n%s\n", err, out)
	}
	// Named-pipe start trigger: equivalent to NETWORK_ENDPOINT trigger on \pipe\cowork-vm-service.
	if out, err := runSC("triggerinfo", coworkServiceName, "start/namedpipe/"+coworkPipeName); err != nil {
		fmt.Printf("Warning: setting service trigger failed: %v\n%s\n", err, out)
	}

	if err := syncCoworkFirewall([]string{binPath}); err != nil {
		fmt.Printf("Warning: configuring Cowork firewall rules failed: %v\n", err)
	}

	fmt.Println("CoworkVMService registered.")
	return nil
}

// coworkFirewallFile records the programs our Cowork firewall rules allow, one per
// line, so they're only rewritten when that changes (netsh's own output is localized).
const coworkFirewallFile = "cowork-firewall.txt"

// syncCoworkFirewall makes our firewall rules mirror the AppxManifest's inbound and
// outbound TCP allow rules for each of programs: the cowork-svc.exe the service is
// configured to run, plus an old copy still running until the service restarts from
// the new folder. Rules of the same name are deleted (ignoring "no rules match") before
// being re-added; nothing is done when they already allow exactly programs.
func syncCoworkFirewall(programs []string) error {
	programs = slices.Sorted(slices.Values(programs))
	want := strings.Join(programs, "\n")
	if readVersionFile(coworkFirewallFile) == want {
		return nil
	}
	deleteCoworkFirewallRules()
	for _, program := range programs {
		for _, r := range []struct{ name, dir string }{{coworkFirewallIn, "in"}, {coworkFirewallOut, "out"}} {
			cmd := utils.Command("netsh", "advfirewall", "firewall", "add", "rule",
				"name="+r.name, "dir="+r.dir, "action=allow",
				"program="+program, "protocol=TCP", "profile=any", "enable=yes")
			if out, err := cmd.CombinedOutput(); err != nil {
				os.Remove(filepath.Join(installBaseDir, coworkFirewallFile)) // retry next time
				return fmt.Errorf("adding %s rule: %v\n%s", r.dir, err, string(out))
			}
		}
	}
	return os.WriteFile(filepath.Join(installBaseDir, coworkFirewallFile), []byte(want), 0644)
}

// ourCoworkService returns CoworkVMService's configuration (sc qc) and whether it's
// ours: its binary is in our install, not an official install's.
func ourCoworkService() (config string, ours bool, err error) {
	config, err = runSC("qc", coworkServiceName)
	return config, strings.Contains(strings.ToLower(config), strings.ToLower(filepath.Clean(utils.WindowsInstallDir))), err
}

// stopCoworkService stops our CoworkVMService and waits (up to ~10 s) until it has.
func stopCoworkService() {
	if _, ours, _ := ourCoworkService(); !ours {
		return
	}
	runSC("stop", coworkServiceName)
	for i := 0; i < 50; i++ {
		if state, _ := runSC("query", coworkServiceName); !strings.Contains(state, "RUNNING") && !strings.Contains(state, "STOP_PENDING") {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// moveCoworkService points our CoworkVMService at folder's cowork-svc.exe, when it
// names another install folder: each version installs into a folder of its own (see
// appfolder_windows.go). A service that's running keeps running the old copy until
// it's stopped (see CleanupOldInstalls). Returns the install folder the service is
// configured to run from afterwards, "" if it isn't ours. Must be called elevated.
func moveCoworkService(folder string) string {
	config, ours, _ := ourCoworkService()
	if !ours {
		return ""
	}
	configured := coworkServiceFolder(config)
	if strings.EqualFold(configured, filepath.Base(folder)) {
		return configured
	}
	binPath := filepath.Join(folder, "resources", "cowork-svc.exe")
	if _, err := os.Stat(binPath); err != nil {
		fmt.Printf("Warning: not moving CoworkVMService, %s is missing: %v\n", binPath, err)
		return configured
	}
	fmt.Printf("Moving CoworkVMService to %s\n", binPath)
	if out, err := runSC("config", coworkServiceName, "binPath=", binPath); err != nil {
		fmt.Printf("Warning: moving CoworkVMService failed: %v\n%s\n", err, out)
		return configured
	}
	return filepath.Base(folder)
}

// coworkServiceFolder is the install folder (its name, e.g. app-latest) that the
// service configuration config (sc qc) runs cowork-svc.exe from, or "".
func coworkServiceFolder(config string) string {
	base := strings.ToLower(filepath.Clean(installBaseDir)) + `\`
	i := strings.Index(strings.ToLower(config), base)
	if i < 0 {
		return ""
	}
	folder, _, _ := strings.Cut(config[i+len(base):], `\`)
	return folder
}

// RemoveCoworkService undoes RegisterCoworkService: it stops and deletes
// CoworkVMService only if it's ours (an official one is left alone), and removes our
// firewall rules either way. Must be called elevated, before the install folder is
// deleted (cowork-svc.exe is locked while it runs).
func RemoveCoworkService() {
	_, ours, qcErr := ourCoworkService()
	if ours {
		fmt.Println("Removing CoworkVMService...")
		stopCoworkService()
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
