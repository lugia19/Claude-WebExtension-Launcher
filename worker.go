package main

import (
	"claude-webext-patcher/extensions"
	"claude-webext-patcher/patcher"
	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
	"fmt"
	"io"
	"os"
)

// The worker is the launcher re-run with --worker. It does the part of the update
// that writes to the install: patching Claude, updating extensions, and (Windows)
// registering the Cowork service. On Windows it's started elevated through UAC; on
// macOS and Linux it's a plain child process. Either way it has no console: it
// appends to the launcher's log file and reports each step through a status file the
// launcher's window follows.

type workerOptions struct {
	statusFile     string
	logFile        string
	installVersion string // Claude version to install; empty to leave Claude as is
	installURL     string
	packagePath    string // the package the launcher downloaded for installVersion
	cowork         bool   // register the Cowork service (Windows)
	uninstall      bool   // remove the patched Claude instead (see uninstall.go)
	debug          bool
}

// runWorker does the work and returns the process exit code: 0, or 1 if Claude
// couldn't be patched. Extension and Cowork failures are reported as warnings only.
func runWorker(o workerOptions) int {
	var console io.Writer
	if o.debug {
		ensureConsole()
		console = os.Stdout
	}
	stop, err := utils.StartLog(o.logFile, false, console)
	defer stop()
	if err != nil {
		fmt.Printf("Could not open the log file %s: %v\n", o.logFile, err)
	}
	fmt.Println("--- Worker ---")

	st, err := status.Create(o.statusFile)
	if err != nil {
		fmt.Printf("Could not open the status file: %v\n", err) // st is nil: reports are dropped
	}
	defer st.Close()

	if o.uninstall {
		return uninstallWorker(st)
	}

	if err := workerBefore(); err != nil {
		fmt.Printf("Worker setup failed: %v\n", err)
		st.Report(status.StepPatch, status.Failed, err.Error())
		return 1
	}
	defer workerAfter()

	exitCode := 0
	if o.installVersion != "" {
		st.Report(status.StepPatch, status.Running, "")
		patcher.PrefetchedPackage = o.packagePath
		patcher.PrefetchedVersion = o.installVersion
		if err := patcher.Install(o.installVersion, o.installURL); err != nil {
			fmt.Printf("Installing Claude %s failed: %v\n", o.installVersion, err)
			st.Report(status.StepPatch, status.Failed, err.Error())
			exitCode = 1
		} else {
			st.Report(status.StepPatch, status.Done, "")
		}
	} else {
		st.Report(status.StepPatch, status.Skipped, "")
	}

	st.Report(status.StepExtensions, status.Running, "")
	extErr := extensions.UpdateAll()
	if err := patcher.DeploySentinelExtension(); extErr == nil {
		extErr = err
	}
	if extErr != nil {
		fmt.Printf("Warning: extension update failed: %v\n", extErr)
		st.Report(status.StepExtensions, status.Warning, extErr.Error())
	} else {
		st.Report(status.StepExtensions, status.Done, "")
	}

	if o.cowork {
		st.Report(status.StepCowork, status.Running, "")
		if err := registerCowork(); err != nil {
			fmt.Printf("Warning: Cowork service registration failed: %v\n", err)
			st.Report(status.StepCowork, status.Warning, err.Error())
		} else {
			st.Report(status.StepCowork, status.Done, "")
		}
	}

	fmt.Println("--- Worker done ---")
	return exitCode
}
