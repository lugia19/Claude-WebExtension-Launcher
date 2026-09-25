// Package gui shows the launcher's status window: what it's doing, a progress bar
// for downloads, and any error, so the launcher doesn't need a terminal.
//
// SPIKE: evaluating gogpu/ui (pure Go, no cgo on any platform).
package gui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	_ "github.com/gogpu/gg/gpu" // GPU-accelerated drawing; falls back to CPU

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/progressbar"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/material3"
	"github.com/gogpu/ui/widget"
)

// Status is the handle the launcher's work uses to update the window. Safe for
// concurrent use; every setter just updates a signal and requests a redraw.
type Status struct {
	gogpuApp *gogpu.App

	step     state.Signal[string]
	detail   state.Signal[string]
	progress state.Signal[float64]
	noClose  state.Signal[bool]
}

// Step sets the headline ("Downloading Claude 2.7032.0...").
func (s *Status) Step(text string) {
	s.step.Set(text)
	s.gogpuApp.RequestRedraw()
}

// Progress sets the bar, 0..1. Pass a negative value to empty it.
func (s *Status) Progress(frac float64) {
	if frac < 0 {
		frac = 0
	}
	s.progress.Set(frac)
	s.gogpuApp.RequestRedraw()
}

// Detail sets the small line under the bar (latest log line, byte counts...).
func (s *Status) Detail(text string) {
	s.detail.Set(text)
	s.gogpuApp.RequestRedraw()
}

// Run shows the status window and runs work on a background goroutine; it must be
// called from the main goroutine. Everything work prints to stdout/stderr is mirrored
// into the window's detail line (and still written to the original stdout). On success
// the window closes by itself; on error it shows the error and waits to be closed.
//
// If the window can't be created at all (no display, no usable GPU or software
// fallback), work runs without it and ok is false, so the caller can fall back to
// its terminal behaviour for the next launch.
func Run(title string, work func(s *Status) error) (workErr error, ok bool) {
	m3 := material3.New(widget.Hex(0xD97757)) // Claude's orange

	gogpuApp := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(title).
		WithSize(560, 220).
		WithResizable(false))

	s := &Status{
		gogpuApp: gogpuApp,
		step:     state.NewSignal("Starting..."),
		detail:   state.NewSignal(""),
		progress: state.NewSignal(0.0),
		noClose:  state.NewSignal(true),
	}

	uiApp := app.New(
		app.WithWindowProvider(gogpuApp),
		app.WithPlatformProvider(gogpuApp),
		app.WithEventSource(gogpuApp.EventSource()),
		app.WithTheme(m3.AsTheme()),
	)
	uiApp.SetRoot(buildUI(s, gogpuApp))

	restore := captureOutput(s)

	var result error
	finished := make(chan struct{})
	runReturned := make(chan struct{})
	go func() {
		defer close(finished)
		result = work(s)
		if result != nil {
			s.Step("Something went wrong")
			s.Detail(result.Error())
			s.noClose.Set(false) // enable the Close button
			gogpuApp.RequestRedraw()
			return
		}
		// Keep asking until the loop is gone: a Quit that lands before desktop.Run
		// has started its loop would otherwise be lost.
		for {
			gogpuApp.Quit()
			select {
			case <-runReturned:
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()

	runErr := desktop.Run(gogpuApp, uiApp)
	close(runReturned)
	restore()

	select {
	case <-finished:
		return result, runErr == nil
	default:
	}

	if runErr != nil {
		// The window never came up (or died): finish the work without it.
		fmt.Fprintf(os.Stderr, "Status window unavailable (%v), continuing without it\n", runErr)
		<-finished
		return result, false
	}
	// Window closed by the user while work was still running: let it finish.
	<-finished
	return result, true
}

func buildUI(s *Status, gogpuApp *gogpu.App) widget.Widget {
	card := primitives.VBox(
		primitives.Text("Claude WebExtension Launcher").
			FontSize(13).
			Color(widget.RGBA8(120, 120, 120, 255)),
		primitives.Text("").ContentSignal(s.step).
			FontSize(18).
			Bold().
			Color(widget.RGBA8(33, 33, 33, 255)),
		progressbar.New(
			progressbar.ValueSignal(s.progress),
			progressbar.Height(8),
			progressbar.Radius(4),
		),
		primitives.Text("").ContentSignal(s.detail).
			FontSize(12).
			Color(widget.RGBA8(100, 100, 100, 255)),
		button.New(
			button.TextOpt("Close"),
			button.DisabledSignal(s.noClose),
			button.OnClick(gogpuApp.Quit),
		),
	).
		Padding(24).
		Gap(10).
		Background(widget.RGBA8(250, 249, 245, 255))

	return card
}

// captureOutput mirrors stdout/stderr into the window's detail line, keeping the
// original streams as well. Returns a function that restores them.
func captureOutput(s *Status) func() {
	origOut, origErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	os.Stdout, os.Stderr = w, w

	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(io.TeeReader(r, origOut))
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				s.Detail(truncate(line, 90))
			}
		}
	}()

	return func() {
		os.Stdout, os.Stderr = origOut, origErr
		w.Close()
		<-done
		r.Close()
	}
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}
