// Package gui is the launcher's window: a checklist of what the launcher is doing,
// download progress, questions, errors, and a short countdown once Claude is
// running. It's pure Go on every platform (gogpu/ui, no cgo), so builds still
// cross-compile from anywhere.
package gui

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/gogpu/gg"
	_ "github.com/gogpu/gg/gpu" // GPU-accelerated drawing; falls back to CPU
	"github.com/gogpu/gogpu"
	ui "github.com/gogpu/ui"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/theme/material3"
	"github.com/gogpu/ui/widget"

	"claude-webext-patcher/utils"
)

const (
	windowWidth  = 520
	windowHeight = 380
	countdown    = 5 // seconds the window stays up after Claude is launched
)

// window holds what the screens need to switch between each other.
type window struct {
	gogpuApp *gogpu.App
	uiApp    *app.App
	s        *Status

	mu     sync.Mutex
	queued []func()
	bg     sync.WaitGroup // background jobs Run waits for (see background)

	// Instance list state (instances.go).
	inst      *Instances
	notes     map[string]*text // the current list's per-row notes, by instance name
	note      map[string]string
	launching map[string]int // launches still in progress, per instance (not deletable yet)
}

// runOnUI runs fn on the UI thread, where changing the root or focus is safe (gogpu/ui
// has no way to post work there itself). fn runs at the start of the next frame;
// calls queued before the window loop starts wait for it.
func (w *window) runOnUI(fn func()) {
	w.mu.Lock()
	w.queued = append(w.queued, fn)
	w.mu.Unlock()
	w.gogpuApp.RequestRedraw() // wakes the loop, which then calls drain
}

// background runs fn on its own goroutine, and Run waits for it before returning:
// closing the window mustn't cut a launch, a delete or a settings save short (the
// process exits once Run returns).
func (w *window) background(fn func()) {
	w.bg.Add(1)
	go func() {
		defer w.bg.Done()
		fn()
	}()
}

// setRoot switches the window to another screen. UI thread only.
//
// On Linux (GLES), gogpu/ui sometimes shows a stale picture after a switch (an
// earlier screen) until something on the new one repaints. So once the new screen has
// been drawn, everything on it is marked for redrawing, like a hover would. Doing it
// in the same frame is too early: it has to come after that frame's draw.
func (w *window) setRoot(root widget.Widget) {
	w.uiApp.SetRoot(root)
	w.runOnUI(func() { // start of the next frame: still before this one's draw
		w.runOnUI(func() { // the frame after: the switch has been drawn
			if w.uiApp.Window().Root() == root {
				markTreeForRedraw(root)
			}
		})
	})
}

// markTreeForRedraw marks every widget in the tree as needing a repaint, and drops
// the cached pictures of those that keep one (repaint boundaries).
func markTreeForRedraw(wd widget.Widget) {
	type redrawable interface{ SetNeedsRedraw(bool) }
	type sceneCached interface{ InvalidateScene() }
	if r, ok := wd.(redrawable); ok {
		r.SetNeedsRedraw(true)
	}
	if c, ok := wd.(sceneCached); ok {
		c.InvalidateScene()
	}
	for _, child := range wd.Children() {
		markTreeForRedraw(child)
	}
}

// drain runs the queued calls; it's gogpu's OnUpdate, which runs on the UI thread
// once per frame, before drawing.
func (w *window) drain() {
	w.mu.Lock()
	fns := w.queued
	w.queued = nil
	w.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
	if len(fns) > 0 {
		w.gogpuApp.RequestRedraw()
	}
}

// Run shows the window with the given checklist rows and runs work on another
// goroutine; call it from the main goroutine. When work succeeds the window counts
// down and closes (unless the user opens the log); when it fails the window shows the
// error until closed. If the window can't open at all (no display, no renderer), work
// still runs, and a failure is reported through a native dialog instead.
//
// With a non-nil setup, the window opens on the setup screen instead, and work only
// starts once Continue has been clicked (and setup.Apply has run). Closing the window
// on the setup screen ends the launch without doing anything. If the window can't
// open, setup is skipped (Apply never runs, so it's offered again next time) and work
// runs as usual.
//
// With non-nil instances that are Enabled, a successful work is followed by the
// instance list instead of the countdown; the window then stays until the user closes
// it. If the window can't open, instances.Headless is launched instead.
func Run(title string, rows []Row, logPath string, setup *Setup, instances *Instances, work func(s *Status) error) error {
	// gogpu logs through slog; keep it out of the user's way (it goes to the log file,
	// since stdout/stderr are redirected there), and quiet unless something's wrong.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	gogpu.SetLogger(logger)
	ui.SetLogger(logger)
	gg.SetLogger(logger)

	if os.Getenv("GOGPU_GRAPHICS_API") == "" && defaultGraphicsAPI != "" {
		os.Setenv("GOGPU_GRAPHICS_API", defaultGraphicsAPI) // read by gogpu.DefaultConfig
	}

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
	w := &window{gogpuApp: gogpuApp, uiApp: uiApp, s: s, inst: instances}
	gogpuApp.OnUpdate(func(float64) { w.drain() })

	checklist := s.build()
	s.hideButtons()
	setupDone := make(chan []bool, 1)
	if setup != nil {
		w.setRoot(buildSetup(setup, "Continue", func(checked []bool) {
			setupDone <- checked
			w.setRoot(checklist)
		}, nil))
	} else {
		w.setRoot(checklist)
	}

	var result error
	var skippedWork bool
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if setup != nil {
			select {
			case checked := <-setupDone:
				setup.Apply(checked)
			case <-s.closed:
				skippedWork = true // closed on the setup screen, or the window never opened
				return
			}
		}
		result = work(s)
		switch {
		case result != nil:
			s.showError(result)
		case instances != nil && instances.Enabled():
			w.runOnUI(w.showList)
			<-s.closed // the user closes the window when done
			return
		default:
			s.countDown(countdown)
		}
		quit(gogpuApp, s.closed)
	}()

	windowErr := desktop.Run(gogpuApp, uiApp)
	s.markClosed()
	<-finished // also covers the user closing the window while work is still running
	w.bg.Wait()

	if windowErr != nil {
		fmt.Printf("Window unavailable (%v); ran without it\n", windowErr)
		if skippedWork {
			result = work(s)
		}
		if result == nil && instances != nil && instances.Enabled() {
			result = instances.Launch(instances.Headless)
		}
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
