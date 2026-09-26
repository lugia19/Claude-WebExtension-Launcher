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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Version is the current version of the application
const Version = "3.3.3"

// The main instance is the one used when --instance is not given. Only it shares
// Cowork/Code sessions with the official install; other instances stay isolated (see
// SetupSessionSharing / RepairSessionSharing). It used to be called "modified"; its
// data folder is renamed on first run (migrateMainInstance), and --instance modified
// still means it.
const (
	mainInstanceName       = "Main"
	legacyMainInstanceName = "modified"
)

// mainInstance is the main instance's name for this run: mainInstanceName, unless
// its data folder couldn't be migrated yet (see migrateMainInstance).
var mainInstance = mainInstanceName

type launcherOptions struct {
	forceUpdate   bool
	instance      string
	instanceGiven bool // --instance was passed (so no instance list)
	debug         bool // no window: everything in the terminal, Claude attached to it
	logPath       string
	list          bool   // end on the instance list instead of launching o.instance
	installedFrom string // the copy that handed over to this installed one (see install.go)
}

func main() {
	gui.Icon, _ = EmbeddedFS.ReadFile("resources/icons/app.png")
	forceUpdate := flag.Bool("force-update", false, "Re-download and re-patch Claude even if already up to date")
	instanceName := flag.String("instance", "", "Instance to launch, each with its own data (default: "+mainInstanceName+")")
	debug := flag.Bool("debug", false, "No window: show all output in the terminal and run Claude attached to it")

	// Internal: the launcher re-runs itself with these to start the worker.
	worker := flag.Bool("worker", false, "Run as the install worker (internal)")
	statusFile := flag.String("status-file", "", "Worker status file (internal)")
	logFile := flag.String("log-file", "", "Log file to append to (internal)")
	installVersion := flag.String("install-version", "", "Claude version to install (internal)")
	installURL := flag.String("install-url", "", "Download URL of --install-version (internal)")
	packagePath := flag.String("package", "", "Downloaded package for --install-version (internal)")
	cowork := flag.Bool("cowork", false, "Register the Cowork service (internal)")

	showSetup := flag.Bool("show-setup", false, "Show the setup screen again (applications menu, start at login, multiple instances) before launching")
	installedFrom := flag.String(installedFromFlag, "", "The launcher copy that handed over to this installed one (internal)")
	uninstall := flag.Bool("uninstall", false, "Uninstall the launcher, the patched Claude and their shortcuts (asks first)")
	flag.Parse()
	instanceGiven := false
	flag.Visit(func(f *flag.Flag) { instanceGiven = instanceGiven || f.Name == "instance" })

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
			uninstall:      *uninstall,
			debug:          *debug,
		}))
	}
	if *uninstall {
		os.Exit(runUninstall(*debug))
	}

	// The main instance's actual name is only known after migrateMainInstance, which
	// needs the log running; its log file doesn't depend on it.
	// Case-insensitively: on Windows and macOS "main" is the same folder as "Main" (and
	// instanceNameProblem never lets another instance use a case variant of them).
	isMain := *instanceName == "" || strings.EqualFold(*instanceName, mainInstanceName) ||
		strings.EqualFold(*instanceName, legacyMainInstanceName)
	logInstance := *instanceName
	if isMain {
		logInstance = mainInstanceName
	}
	opts := launcherOptions{
		forceUpdate:   *forceUpdate,
		instance:      *instanceName,
		instanceGiven: instanceGiven,
		debug:         *debug,
		logPath:       utils.LogPath(logInstance, mainInstanceName),
		installedFrom: *installedFrom,
	}
	var console io.Writer
	if opts.debug {
		ensureConsole()
		console = os.Stdout
	}
	// After a hand-over (below), the installed copy adds to the log the other started.
	stop, _ := utils.StartLog(opts.logPath, opts.installedFrom == "", console)
	// Before the window: on Windows this restarts a freshly updated .new.exe as the
	// real .exe, and setup must run there so shortcuts don't point at the temporary file.
	selfupdate.FinishUpdateIfNeeded()

	// Run from the installed copy (install.go), installing or upgrading it first if this
	// one is newer.
	if target, from := installSelf(opts.installedFrom != ""); target != "" {
		stop() // flush the log for the installed copy to continue
		err := handOff(target, handOffArgs(from), opts.debug)
		stop, _ = utils.StartLog(opts.logPath, false, console)
		fmt.Printf("Warning: could not start the installed launcher (%v); running from here this time\n", err)
	}

	migrateMainInstance()
	if isMain {
		opts.instance = mainInstance
	}

	if opts.debug {
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

	// Without --instance, the run can end on the instance list; whether it does is
	// decided after the first-run setup, which can turn the list on.
	var instances *gui.Instances
	if !opts.instanceGiven {
		instances = instanceList()
		instances.Enabled = func() bool { return opts.list }
	}
	listLikely := instances != nil && utils.LoadSettings().ManageInstances
	rows := checklistRows(sandboxNeeded(), coworkNeeded(), !listLikely)
	err := gui.Run(gui.Options{
		Title:     "Claude WebExtension Launcher",
		Rows:      rows,
		LogPath:   opts.logPath,
		Setup:     firstRunSetup(*showSetup),
		Instances: instances,
	}, func(s *gui.Status) error {
		ui = s
		patcher.DownloadProgress = s.DownloadProgress
		opts.list = instances != nil && utils.LoadSettings().ManageInstances
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
	platformSetup(o.installedFrom)
	if o.installedFrom != "" {
		refreshShortcuts() // they may point at the copy that handed over
	}
	if o.instanceGiven {
		rememberInstance(o.instance)
	}

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
	// In list mode o.instance is the default one; launchInstance repeats this for
	// whichever instance is started.
	RepairSessionSharing(o.instance)
	SetupSessionSharing(o.instance)

	// Check for official Claude MSIX installation (Windows only)
	checkMSIXAndPrompt(o.instance)

	if o.list {
		ui.SetRow(rowLaunch, status.Skipped, "Launching Claude", "from the instance list") // if shown
		return nil
	}
	clearCaches(o.instance)
	ui.SetRow(rowLaunch, status.Running, "Launching Claude", "")
	if err := launchClaude(o.instance, o.debug); err != nil {
		ui.SetRow(rowLaunch, status.Failed, "Couldn't launch Claude", "")
		return err
	}
	ui.SetRow(rowLaunch, status.Done, "Claude launched", "")
	return nil
}

// launchInstance starts an instance from the instance list: the per-instance tail of
// runLauncher (everything before it is shared by all instances and already done).
func launchInstance(instance string) error {
	RepairSessionSharing(instance) // idempotent; see runLauncher
	SetupSessionSharing(instance)
	clearCaches(instance)
	return launchClaude(instance, false)
}

// runWorkerIfNeeded starts the worker when anything needs writing to the install (a
// new Claude, extension updates, the Cowork service), and follows its progress. A
// failure is fatal only when there's no existing install to fall back to.
func runWorkerIfNeeded(o launcherOptions, update patcher.ClaudeUpdate, pkg string) error {
	// nothingToDo marks the rows when no worker is needed. A failed extension check
	// is a warning, not "up to date".
	nothingToDo := func(extErr error) error {
		ui.SetRow(rowPatch, status.Skipped, "Patching", "up to date")
		ui.SetRow(rowCowork, status.Skipped, "Cowork service", "set up") // no-op if the row isn't shown
		if extErr != nil {
			ui.SetRow(rowExtensions, status.Warning, "Extensions", "couldn't check for updates")
		} else {
			ui.SetRow(rowExtensions, status.Skipped, "Extensions", "up to date")
		}
		return nil
	}

	needCowork := coworkNeeded()
	needExtensions, extErr := extensions.NeedsUpdate()
	if !update.Needed && !needExtensions && !needCowork {
		return nothingToDo(extErr)
	}

	// Serialize with other launchers started at the same time.
	lock, locked := utils.AcquirePatchLock(patchLockName, patchLockTimeout)
	if !locked {
		if claudeInstalled() {
			fmt.Println("Timed out waiting for another launcher to finish updating; launching the existing install.")
			ui.SetRow(rowPatch, status.Warning, "Patching", "another launcher is updating")
			ui.SetRow(rowExtensions, status.Skipped, "Extensions", "")
			ui.SetRow(rowCowork, status.Skipped, "Cowork service", "")
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
	needExtensions, extErr = extensions.NeedsUpdate()
	if !update.Needed && !needExtensions && !needCowork {
		return nothingToDo(extErr)
	}
	if !needCowork {
		ui.SetRow(rowCowork, status.Skipped, "Cowork service", "set up") // registered meanwhile
	}

	args := []string{"--log-file=" + o.logPath}
	if update.Needed {
		args = append(args, "--install-version="+update.Latest, "--install-url="+update.URL, "--package="+pkg)
	}
	if needCowork {
		args = append(args, "--cowork")
	}
	if o.debug {
		args = append(args, "--debug")
	}
	res := followWorker(args)

	switch {
	case res.err != nil: // e.g. UAC declined
		fmt.Printf("Could not start the worker: %v\n", res.err)
		if !claudeInstalled() {
			ui.SetRow(rowPatch, status.Failed, "", "")
			return fmt.Errorf("couldn't install Claude: %v", res.err)
		}
		// Nothing the worker was asked to do happened.
		ui.SetRow(rowPatch, status.Warning, "", "not updated: "+res.err.Error())
		ui.SetRow(rowExtensions, status.Warning, "", "not updated")
		ui.SetRow(rowCowork, status.Warning, "", "not set up")
	case res.code != 0:
		detail := res.detail(status.StepPatch)
		if !claudeInstalled() {
			return fmt.Errorf("installing Claude failed: %s", detail)
		}
		ui.SetRow(rowPatch, status.Warning, "", "failed, using the existing install")
	}
	return nil
}

// workerResult is how a worker run ended.
type workerResult struct {
	code   int
	err    error             // it couldn't be started (e.g. UAC declined)
	failed map[string]string // steps it reported as failed, with their details
}

// detail is why step failed, as the worker reported it.
func (r workerResult) detail(step string) string {
	if d := r.failed[step]; d != "" {
		return d
	}
	return "see the log"
}

// followWorker runs the worker (--worker plus args) and mirrors the steps it reports
// to the checklist until it exits.
func followWorker(args []string) workerResult {
	statusPath := filepath.Join(os.TempDir(), fmt.Sprintf("claude-webext-status-%d.jsonl", os.Getpid()))
	os.Remove(statusPath)
	defer os.Remove(statusPath)

	done := make(chan workerResult, 1)
	go func() {
		code, err := startWorker(append([]string{"--worker", "--status-file=" + statusPath}, args...))
		done <- workerResult{code: code, err: err}
	}()

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
	for {
		select {
		case res := <-done:
			apply()
			res.failed = failed
			return res
		case <-tick.C:
			apply()
		}
	}
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

func launchClaude(instance string, debug bool) error {
	claudePath := claudeExecutablePath()
	cmd := exec.Command(claudePath, "--instance="+instance)
	cmd.Dir = filepath.Dir(claudePath)
	fmt.Printf("Launching Claude (instance %q).\n", instance)

	if debug {
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

// firstRunSetup returns the setup screen for the first launch (or when asked for with
// --show-setup), or nil once it has been shown.
func firstRunSetup(force bool) *gui.Setup {
	if utils.LoadSettings().SetupDone && !force {
		return nil
	}
	return launcherSettings("Welcome to the WebExtension Launcher", []string{
		"A couple of choices before the first launch.",
		"You can change them later by running the launcher with --show-setup.",
	})
}

// launcherSettings is the setup screen: the launcher's own menu and startup entries,
// and whether it shows the instance list. The shortcut checkboxes start from what's
// already on disk, so shortcuts made with the old Toggle-*.bat scripts are reflected;
// unchecking one removes it. They're left out where there are no such shortcuts to
// make (macOS). Instances' own entries are set from the list.
func launcherSettings(title string, subtitle []string) *gui.Setup {
	settings := utils.LoadSettings()
	shortcuts := menuEntrySupported()
	var options []gui.SetupOption
	if shortcuts {
		options = append(options,
			// Suggested on the first run; reopened, it shows what's there, so Continue
			// doesn't recreate an entry the user removed.
			gui.SetupOption{Label: "Add to the applications menu", Checked: !settings.SetupDone || hasMenuEntry(launcherEntry)},
			gui.SetupOption{Label: "Start when I log in", Checked: hasStartup(launcherEntry)},
		)
	}
	options = append(options, gui.SetupOption{
		Label:   "Manage multiple instances (separate logins and data)",
		Checked: settings.ManageInstances,
	})
	var extra []gui.SetupButton
	if settings.SetupDone { // not on the very first run: there's nothing to uninstall yet
		extra = append(extra, gui.SetupButton{Label: "Uninstall…", OnClick: startUninstall, CloseWindow: true})
	}
	return &gui.Setup{
		Title:    title,
		Subtitle: subtitle,
		Options:  options,
		Extra:    extra,
		Apply: func(checked []bool) {
			if shortcuts {
				applyShortcuts(launcherEntry, checked[0], checked[1])
			}
			manage := checked[len(checked)-1]
			err := utils.UpdateSettings(func(s *utils.Settings) {
				s.SetupDone = true
				s.ManageInstances = manage
			})
			if err != nil {
				fmt.Printf("Warning: could not save settings: %v\n", err)
			}
		},
	}
}

// applyShortcuts makes an entry's menu and startup shortcuts (see shortcuts.go) match
// the given choices.
func applyShortcuts(entry string, menu, startup bool) {
	apply := func(what string, want, have bool, add, remove func() error) {
		var err error
		switch {
		case want: // rewrite even if it exists: it may point at an old launcher path
			err = add()
		case have:
			err = remove()
		default:
			return
		}
		if err != nil {
			fmt.Printf("Warning: could not update the %s: %v\n", what, err)
		}
	}
	apply("applications menu entry", menu, hasMenuEntry(entry),
		func() error { return addMenuEntry(entry) },
		func() error { return removeMenuEntry(entry) })
	apply("startup entry", startup, hasStartup(entry),
		func() error { return setStartup(entry, true) },
		func() error { return setStartup(entry, false) })
}
