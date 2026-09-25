package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/gui"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/selfupdate"
	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Version is the current version of the application
const Version = "3.3.3"

// defaultInstanceName is the instance used when --instance is not given. Only this
// instance shares Cowork/Code sessions with the official install; named instances stay
// isolated (see SetupSessionSharing / RepairSessionSharing).
const defaultInstanceName = "modified"

type launcherOptions struct {
	forceUpdate bool
	instance    string
	debug       bool // no window: everything in the terminal, Claude attached to it
	logPath     string
}

func main() {
	forceUpdate := flag.Bool("force-update", false, "Re-download and re-patch Claude even if already up to date")
	instanceName := flag.String("instance", defaultInstanceName, "Instance name for separate data directory and lock")
	debug := flag.Bool("debug", false, "No window: show all output in the terminal and run Claude attached to it")

	// Internal: the launcher re-runs itself with these to start the worker.
	worker := flag.Bool("worker", false, "Run as the install worker (internal)")
	statusFile := flag.String("status-file", "", "Worker status file (internal)")
	logFile := flag.String("log-file", "", "Log file to append to (internal)")
	installVersion := flag.String("install-version", "", "Claude version to install (internal)")
	installURL := flag.String("install-url", "", "Download URL of --install-version (internal)")
	packagePath := flag.String("package", "", "Downloaded package for --install-version (internal)")
	cowork := flag.Bool("cowork", false, "Register the Cowork service (internal)")
	flag.Parse()

	selfupdate.CurrentVersion = Version
	patcher.EmbeddedFS = EmbeddedFS
	patcher.Debug = *debug

	if *worker {
		os.Exit(runWorker(workerOptions{
			statusFile:     *statusFile,
			logFile:        *logFile,
			installVersion: *installVersion,
			installURL:     *installURL,
			packagePath:    *packagePath,
			cowork:         *cowork,
			debug:          *debug,
		}))
	}

	opts := launcherOptions{
		forceUpdate: *forceUpdate,
		instance:    *instanceName,
		debug:       *debug,
		logPath:     utils.LogPath(*instanceName, defaultInstanceName),
	}

	if opts.debug {
		ensureConsole()
		stop, _ := utils.StartLog(opts.logPath, true, os.Stdout)
		err := runLauncher(opts)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		stop()
		if err != nil {
			os.Exit(1)
		}
		return
	}

	stop, _ := utils.StartLog(opts.logPath, true, nil)
	rows := checklistRows(sandboxNeeded(), coworkNeeded())
	err := gui.Run("Claude WebExtension Launcher", rows, opts.logPath, func(s *gui.Status) error {
		ui = s
		patcher.DownloadProgress = s.DownloadProgress
		selfupdate.Notify = func(title, detail string) { s.Ask(title, detail, []string{"OK"}) }
		return runLauncher(opts)
	})
	stop()
	if err != nil {
		os.Exit(1)
	}
}

// runLauncher is the launcher's whole flow: update itself, make sure Claude is
// downloaded, patched and has current extensions (via the worker), then start it.
// Every step is mirrored to the checklist through ui.
func runLauncher(o launcherOptions) error {
	fmt.Printf("Claude WebExtension Launcher %s, %s\n", Version, time.Now().Format(time.RFC1123))
	selfupdate.FinishUpdateIfNeeded()
	platformSetup()

	// Launcher self-update (restarts the launcher if it installs one).
	ui.SetRow(rowLauncher, status.Running, "Checking for launcher updates", "")
	if err := selfupdate.CheckAndUpdate(); err != nil {
		fmt.Printf("Launcher update check failed: %v\n", err)
		ui.SetRow(rowLauncher, status.Warning, "Couldn't check for launcher updates", "")
	} else {
		ui.SetRow(rowLauncher, status.Done, "Launcher up to date", Version)
	}

	// Claude: check, and download without elevation if an update is needed.
	ui.SetRow(rowClaude, status.Running, "Checking for Claude updates", "")
	update, err := patcher.CheckClaude(o.forceUpdate)
	var pkg string
	switch {
	case err != nil && claudeInstalled():
		fmt.Printf("Claude update check failed: %v\n", err)
		ui.SetRow(rowClaude, status.Warning, "Couldn't check for Claude updates", "using "+update.Installed)
		update.Needed = false
	case err != nil:
		ui.SetRow(rowClaude, status.Failed, "Couldn't check for Claude updates", "")
		return fmt.Errorf("couldn't find the latest Claude release: %v", err)
	case !update.Needed:
		ui.SetRow(rowClaude, status.Skipped, "Claude "+update.Installed, "up to date")
	default:
		fmt.Printf("Claude %s needs installing (%s)\n", update.Latest, update.Reason)
		ui.SetRow(rowClaude, status.Running, "Downloading Claude "+update.Latest, "")
		pkg, err = patcher.Prefetch(update.Latest, update.URL)
		switch {
		case err != nil && claudeInstalled():
			fmt.Printf("Download failed: %v\n", err)
			ui.SetRow(rowClaude, status.Warning, "Couldn't download Claude "+update.Latest, "using "+update.Installed)
			update.Needed = false
		case err != nil:
			ui.SetRow(rowClaude, status.Failed, "Couldn't download Claude "+update.Latest, "")
			return fmt.Errorf("downloading Claude: %v", err)
		default:
			defer os.Remove(pkg)
			ui.SetRow(rowClaude, status.Done, "Downloaded Claude "+update.Latest, "")
		}
	}

	// Linux: the one-time AppArmor profile Claude's sandbox needs on Ubuntu 24.04+.
	if sandboxNeeded() {
		ui.SetRow(rowSandbox, status.Running, "Asking for permission to set up the sandbox", "")
		if err := installSandbox(); err != nil {
			ui.SetRow(rowSandbox, status.Warning, "Sandbox not set up", err.Error()+" (see the log)")
		} else {
			ui.SetRow(rowSandbox, status.Done, "Sandbox set up", "")
		}
	}

	if err := runWorkerIfNeeded(o, update, pkg); err != nil {
		return err
	}

	// Reconcile Cowork/Code session sharing before any uninstall prompt (Windows only):
	// repair a named instance that was wrongly pooled by an older build, then (only for the
	// default instance) share with the official install via junctions into a neutral store.
	RepairSessionSharing(o.instance)
	SetupSessionSharing(o.instance)

	// Check for official Claude MSIX installation (Windows only)
	checkMSIXAndPrompt(o.instance)

	clearCaches(o.instance)

	ui.SetRow(rowLaunch, status.Running, "Launching Claude", "")
	if err := launchClaude(o); err != nil {
		ui.SetRow(rowLaunch, status.Failed, "Couldn't launch Claude", "")
		return err
	}
	ui.SetRow(rowLaunch, status.Done, "Claude launched", "")
	return nil
}

// runWorkerIfNeeded starts the worker when anything needs writing to the install (a
// new Claude, extension updates, the Cowork service), and follows its progress. A
// failure is fatal only when there's no existing install to fall back to.
func runWorkerIfNeeded(o launcherOptions, update patcher.ClaudeUpdate, pkg string) error {
	needCowork := coworkNeeded()
	needExtensions := extensions.NeedsUpdate()
	if !update.Needed && !needExtensions && !needCowork {
		ui.SetRow(rowPatch, status.Skipped, "Patching", "up to date")
		ui.SetRow(rowExtensions, status.Skipped, "Extensions", "up to date")
		return nil
	}

	// Serialize with other launchers started at the same time.
	lock, locked := utils.AcquirePatchLock(patchLockName, patchLockTimeout)
	if !locked {
		if claudeInstalled() {
			fmt.Println("Timed out waiting for another launcher to finish updating; launching the existing install.")
			ui.SetRow(rowPatch, status.Warning, "Patching", "another launcher is updating")
			ui.SetRow(rowExtensions, status.Skipped, "Extensions", "")
			return nil
		}
		fmt.Println("Waiting for another launcher to finish installing Claude...")
		if lock, locked = utils.AcquirePatchLock(patchLockName, 24*time.Hour); !locked {
			return fmt.Errorf("could not acquire the install lock")
		}
	}
	defer lock.Release()

	// Another launcher may have done some or all of the work while we waited for the
	// lock (each downloads to its own file, so they don't trip over each other before
	// this). Check again, so we don't run a redundant worker (and UAC prompt).
	if update.Needed && !o.forceUpdate && patcher.IsInstalled(update.Latest) {
		fmt.Printf("Claude %s was installed by another launcher in the meantime\n", update.Latest)
		update.Needed = false
	}
	needCowork = coworkNeeded()
	needExtensions = extensions.NeedsUpdate()
	if !update.Needed && !needExtensions && !needCowork {
		ui.SetRow(rowPatch, status.Skipped, "Patching", "up to date")
		ui.SetRow(rowExtensions, status.Skipped, "Extensions", "up to date")
		return nil
	}

	statusPath := filepath.Join(os.TempDir(), fmt.Sprintf("claude-webext-status-%d.jsonl", os.Getpid()))
	os.Remove(statusPath)
	defer os.Remove(statusPath)

	args := []string{"--worker", "--status-file=" + statusPath, "--log-file=" + o.logPath}
	if update.Needed {
		args = append(args, "--install-version="+update.Latest, "--install-url="+update.URL, "--package="+pkg)
	}
	if needCowork {
		args = append(args, "--cowork")
	}
	if o.debug {
		args = append(args, "--debug")
	}

	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := startWorker(args)
		done <- result{code, err}
	}()

	// Follow the worker's status file until it exits.
	reader := status.NewReader(statusPath)
	failed := map[string]string{}
	apply := func() {
		for _, e := range reader.Poll() {
			if e.State == status.Failed {
				failed[e.Step] = e.Detail
			}
			ui.SetRow(e.Step, e.State, "", e.Detail)
		}
	}
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	var res result
wait:
	for {
		select {
		case res = <-done:
			apply()
			break wait
		case <-tick.C:
			apply()
		}
	}

	switch {
	case res.err != nil: // e.g. UAC declined
		fmt.Printf("Could not start the worker: %v\n", res.err)
		if !claudeInstalled() {
			ui.SetRow(rowPatch, status.Failed, "", "")
			return fmt.Errorf("couldn't install Claude: %v", res.err)
		}
		ui.SetRow(rowPatch, status.Warning, "", "not updated: "+res.err.Error())
	case res.code != 0:
		detail := failed[status.StepPatch]
		if !claudeInstalled() {
			return fmt.Errorf("installing Claude failed: %s", detail)
		}
		ui.SetRow(rowPatch, status.Warning, "", "failed, using the existing install")
	}
	return nil
}

func clearCaches(instance string) {
	dir := claudeUserDataDir(instance)
	fmt.Println("Clearing cache folders:")
	for _, sub := range []string{"Service Worker", "WebStorage", "Cache", "Code Cache"} {
		p := filepath.Join(dir, sub)
		fmt.Printf("  %s\n", p)
		os.RemoveAll(p)
	}
}

func launchClaude(o launcherOptions) error {
	claudePath := claudeExecutablePath()
	cmd := exec.Command(claudePath, "--instance="+o.instance)
	cmd.Dir = filepath.Dir(claudePath)
	fmt.Println("Launching Claude.")

	if o.debug {
		// Run Claude in this terminal to see its output.
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		return cmd.Run()
	}
	detachFromTerminal(cmd)
	return cmd.Start()
}

// claudeInstalled reports whether a patched Claude is present to fall back on.
func claudeInstalled() bool {
	_, err := os.Stat(claudeExecutablePath())
	return err == nil
}
