//go:build windows

package patcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	return exec.Command("sc.exe", "query", coworkServiceName).Run() == nil
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

	if err := configureCoworkFirewall(binPath); err != nil {
		fmt.Printf("Warning: configuring Cowork firewall rules failed: %v\n", err)
	}

	fmt.Println("CoworkVMService registered.")
	return nil
}

// configureCoworkFirewall mirrors the AppxManifest's inbound+outbound TCP allow rules for
// cowork-svc.exe. Idempotent: stable rule names are deleted (ignoring "no rules match")
// before being re-added.
func configureCoworkFirewall(binPath string) error {
	exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+coworkFirewallIn).Run()
	exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+coworkFirewallOut).Run()

	rules := []struct {
		name string
		dir  string
	}{
		{coworkFirewallIn, "in"},
		{coworkFirewallOut, "out"},
	}
	for _, r := range rules {
		cmd := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
			"name="+r.name, "dir="+r.dir, "action=allow",
			"program="+binPath, "protocol=TCP", "profile=any", "enable=yes")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("adding %s rule: %v\n%s", r.dir, err, string(out))
		}
	}
	return nil
}

// runSC runs an sc.exe command and returns its combined output.
func runSC(args ...string) (string, error) {
	out, err := exec.Command("sc.exe", args...).CombinedOutput()
	return string(out), err
}
