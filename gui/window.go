// Package gui is the launcher's window: a checklist of what the launcher is doing,
// download progress, questions, errors, and a short countdown once Claude is
// running. It's pure Go on every platform (gogpu/ui, no cgo), so builds still
// cross-compile from anywhere.
package gui

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gogpu/gg"
	_ "github.com/gogpu/gg/gpu" // GPU-accelerated drawing; falls back to CPU
	"github.com/gogpu/gogpu"
	ui "github.com/gogpu/ui"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/theme/material3"

	"claude-webext-patcher/utils"
)

const (
	windowWidth  = 520
	windowHeight = 380
	countdown    = 5 // seconds the window stays up after Claude is launched
)

// Run shows the window with the given checklist rows and runs work on another
// goroutine; call it from the main goroutine. When work succeeds the window counts
// down and closes (unless the user opens the log); when it fails the window shows the
// error until closed. If the window can't open at all (no display, no renderer), work
// still runs, and a failure is reported through a native dialog instead.
func Run(title string, rows []Row, logPath string, work func(s *Status) error) error {
	// gogpu logs through slog; keep it out of the user's way (it goes to the log file,
	// since stdout/stderr are redirected there), and quiet unless something's wrong.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gogpu.SetLogger(logger)
	ui.SetLogger(logger)
	gg.SetLogger(logger)

	gogpuApp := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(title).
		WithSize(windowWidth, windowHeight).
		WithResizable(false))

	s := newStatus(gogpuApp, rows, logPath)
	uiApp := app.New(
		app.WithWindowProvider(gogpuApp),
		app.WithPlatformProvider(gogpuApp),
		app.WithEventSource(gogpuApp.EventSource()),
		app.WithTheme(material3.NewDark(accent).AsTheme()),
	)
	uiApp.SetRoot(s.build())
	s.hideButtons()

	var result error
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result = work(s)
		if result != nil {
			s.showError(result)
		} else {
			s.countDown(countdown)
		}
		quit(gogpuApp, s.closed)
	}()

	windowErr := desktop.Run(gogpuApp, uiApp)
	s.markClosed()
	<-finished // also covers the user closing the window while work is still running

	if windowErr != nil {
		fmt.Printf("Window unavailable (%v); ran without it\n", windowErr)
		if result != nil {
			utils.ShowErrorDialog(title, fmt.Sprintf("%v\n\nDetails are in the log:\n%s", result, logPath))
		}
	}
	return result
}

// quit ends the window loop. gogpu drops a Quit that arrives before its loop has
// started, so keep asking until the loop is gone.
func quit(a *gogpu.App, closed <-chan struct{}) {
	for {
		a.Quit()
		select {
		case <-closed:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
