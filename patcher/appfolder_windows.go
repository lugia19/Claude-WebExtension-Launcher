//go:build windows

package patcher

import (
	"claude-webext-patcher/utils"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Each install goes into a folder of its own, app-<version> (app-<version>-2, ... when
// that name is taken), and currentAppFile names the current one. Windows can't rename
// or replace a folder while anything in it is in use, so swapping a fixed folder fails
// whenever Claude (or anything else) still has a file of the old install open. A new
// folder needs nothing from the old one; CleanupOldInstalls deletes old ones in a
// later worker run, once they're no longer in use.
const currentAppFile = "current-app.txt"

// Replaceable in tests.
var (
	appFolderProcesses = runningAppFolderProcesses
	moveCowork         = moveCoworkService
	stopCowork         = stopCoworkService
	syncCoworkRules    = syncCoworkFirewall
)

// currentAppFolder is the current install's folder. Installs from before versioned
// folders have no currentAppFile: they're app-latest.
func currentAppFolder() string {
	if name := readVersionFile(currentAppFile); strings.HasPrefix(name, "app-") && filepath.Base(name) == name {
		return filepath.Join(installBaseDir, name)
	}
	return filepath.Join(installBaseDir, appFolderName)
}

// newAppFolder is where buildAndInstall builds version: a folder that doesn't exist
// yet, so never the current install, even when reinstalling the same version.
func newAppFolder(version string) string {
	base := filepath.Join(installBaseDir, "app-"+version)
	folder := base
	for n := 2; ; n++ {
		if _, err := os.Lstat(folder); errors.Is(err, fs.ErrNotExist) {
			return folder
		}
		folder = fmt.Sprintf("%s-%d", base, n)
	}
}

// commitAppFolder makes folder the current install, by naming it in currentAppFile,
// and returns it. The previous install stays where it is.
func commitAppFolder(folder, previous string) (string, error) {
	path := filepath.Join(installBaseDir, currentAppFile)
	if err := utils.WriteFileAtomic(path, []byte(filepath.Base(folder)), 0644); err != nil {
		return previous, fmt.Errorf("recording the new install: %w", err)
	}
	fmt.Printf("Installed to %s\n", folder)
	return folder, nil
}

// CleanupOldInstalls deletes the install folders other than the current one (earlier
// versions, app-latest from before versioned folders, leftovers of failed builds) once
// nothing runs from them. Our Cowork service is first moved to the current folder (if
// that fails, the folder it's still configured for is kept); one still running from
// an old folder is stopped only while no patched Claude runs, so a local Cowork task
// is never cut off, and keeps its firewall allowance until then. A folder still in use
// is kept and retried in a later worker run. Must be called elevated.
func CleanupOldInstalls() {
	current := currentAppFolder()
	serviceFolder := moveCowork(current)
	defer func() {
		if serviceFolder != "" {
			syncServiceFirewall(serviceFolder)
		}
	}()

	entries, err := os.ReadDir(installBaseDir)
	if err != nil {
		return
	}
	running := appFolderProcesses()
	claudeOpen := false
	for _, exes := range running {
		claudeOpen = claudeOpen || exes["claude.exe"]
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, "app-") || strings.EqualFold(name, filepath.Base(current)) {
			continue
		}
		if strings.EqualFold(name, serviceFolder) {
			fmt.Printf("Keeping old install %s for now, CoworkVMService is still set up to run from it\n", name)
			continue
		}
		exes := running[strings.ToLower(name)]
		if len(exes) == 1 && exes["cowork-svc.exe"] && !claudeOpen {
			fmt.Printf("Stopping CoworkVMService, it still runs from %s\n", name)
			stopCowork()
			exes = appFolderProcesses()[strings.ToLower(name)]
		}
		if len(exes) > 0 {
			fmt.Printf("Keeping old install %s for now, it's in use (%s)\n", name, strings.Join(setKeys(exes), ", "))
			continue
		}
		if err := os.RemoveAll(filepath.Join(installBaseDir, name)); err != nil {
			fmt.Printf("Could not fully remove old install %s (will retry next time): %v\n", name, err)
			continue
		}
		fmt.Printf("Removed old install %s\n", name)
	}
}

// syncServiceFirewall points our Cowork firewall rules at the cowork-svc.exe the
// service is configured to run (in serviceFolder), and at any other copy of it still
// running from an old install folder, so a running service keeps its allowance until
// it restarts from the new one.
func syncServiceFirewall(serviceFolder string) {
	exe := func(folder string) string {
		return filepath.Join(installBaseDir, folder, "resources", "cowork-svc.exe")
	}
	programs := []string{exe(serviceFolder)}
	for folder, exes := range appFolderProcesses() {
		if exes["cowork-svc.exe"] && !strings.EqualFold(folder, serviceFolder) {
			programs = append(programs, exe(folder))
		}
	}
	if err := syncCoworkRules(programs); err != nil {
		fmt.Printf("Warning: updating Cowork firewall rules failed: %v\n", err)
	}
}

// runningAppFolderProcesses returns the programs running from each install folder:
// lowercased folder name -> lowercased exe names. Needs elevation to see other
// users' (and the service's) processes.
func runningAppFolderProcesses() map[string]map[string]bool {
	running := map[string]map[string]bool{}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return running
	}
	defer windows.CloseHandle(snapshot)
	base := strings.ToLower(filepath.Clean(installBaseDir)) + `\`
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		path := strings.ToLower(processImagePath(entry.ProcessID))
		rel, ok := strings.CutPrefix(path, base)
		if !ok {
			continue
		}
		folder, _, _ := strings.Cut(rel, `\`)
		if running[folder] == nil {
			running[folder] = map[string]bool{}
		}
		running[folder][filepath.Base(path)] = true
	}
	return running
}

// processImagePath is the full path of a process's executable, or "" if it can't be
// queried.
func processImagePath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

func setKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	return keys
}
