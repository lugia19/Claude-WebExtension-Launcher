// Package gui is the launcher's window: a checklist of what the launcher is doing,
// download progress, questions, errors, and a short countdown once Claude is
// running. It's built on Fyne (OpenGL through GLFW, so it needs cgo).
package gui

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

const (
	windowWidth  = 520
	windowHeight = 380
	countdown    = 5 // seconds the window stays up after Claude is launched
)

// Icon is the window icon (PNG); set it before Run.
var Icon []byte

// window holds what the screens need to switch between each other.
type window struct {
	app fyne.App
	win fyne.Window
	s   *Status
	bg  sync.WaitGroup // background jobs Run waits for (see background)

	// Instance list state (instances.go).
	inst  *Instances
	notes map[string]*widget.Label // the current list's per-row notes, by instance name; UI thread only

	mu        sync.Mutex        // guards note and launching, which background jobs change
	note      map[string]string // each instance's note, kept across list rebuilds
	launching map[string]int    // launches still in progress, per instance (not deletable yet)
}

// runOnUI runs fn on the UI thread, where widgets may be changed. Calls made before
// the window loop starts wait for it; once the window is closing they're dropped.
func (w *window) runOnUI(fn func()) {
	if w.s.isClosed() {
		return
	}
	fyne.Do(fn)
}

// quit closes the window and ends the loop. Status goes quiet first: once the loop
// stops, Fyne would run queued UI calls on the caller's goroutine.
func (w *window) quit() {
	w.s.markClosed()
	fyne.Do(w.app.Quit) // queued: a Quit before the loop has started would be lost
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

// screen pads a screen's content away from the window's edges.
func screen(content fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(16, 16, 20, 20), content)
}

// Options configure Run.
type Options struct {
	Title   string
	Rows    []Row  // the checklist
	LogPath string // for Open logs, and the error dialog

	// Setup, if not nil, is shown first; work only starts once it's confirmed (and
	// Setup.Apply has run). Closing the window on it ends the run without doing
	// anything.
	Setup *Setup

	// Instances that are Enabled: a successful work is followed by the instance list
	// instead of the countdown; the window then stays until the user closes it.
	Instances *Instances

	// Done, if set, replaces the countdown after a successful work: the window shows the
	// message it returns until closed, or just closes if that's empty.
	Done func() string
}

// Run shows the window with the checklist and runs work on another goroutine; call
// it from the main goroutine. When work succeeds the window counts down and closes
// (unless the user opens the log); when it fails the window shows the error until
// closed. See Options for the rest.
func Run(o Options, work func(s *Status) error) error {
	s := newStatus(o.Rows, o.LogPath)
	a := app.NewWithID("com.lugia19.claude-webext-launcher")
	a.Settings().SetTheme(newLauncherTheme())
	if Icon != nil {
		a.SetIcon(fyne.NewStaticResource("icon.png", Icon))
	}
	win := a.NewWindow(o.Title)
	win.SetMaster()
	win.SetFixedSize(true)
	win.Resize(fyne.NewSize(windowWidth, windowHeight))
	win.CenterOnScreen()
	w := &window{app: a, win: win, s: s, inst: o.Instances, note: map[string]string{}, launching: map[string]int{}}
	s.w = w
	win.SetOnClosed(s.markClosed)

	checklist := s.build()
	setupDone := make(chan []bool, 1)
	if o.Setup != nil {
		win.SetContent(w.buildSetup(o.Setup, "Continue", func(checked []bool) {
			setupDone <- checked
			win.SetContent(checklist)
		}, nil))
	} else {
		win.SetContent(checklist)
	}

	var result error
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if o.Setup != nil {
			select {
			case checked := <-setupDone:
				o.Setup.Apply(checked)
			case <-s.closed:
				return // closed on the setup screen
			}
		}
		result = work(s)
		switch {
		case result != nil:
			s.showError(result)
		case o.Instances != nil && o.Instances.Enabled():
			w.runOnUI(w.showList)
			return // the user closes the window when done
		case o.Done != nil:
			if msg := o.Done(); msg != "" {
				s.showDone(msg)
			}
		default:
			s.countDown(countdown)
		}
		w.quit()
	}()

	win.ShowAndRun()
	s.markClosed()
	<-finished // also covers the user closing the window while work is still running
	w.bg.Wait()
	return result
}
