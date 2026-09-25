// Package gui is the launcher's status window: the current step, a progress bar,
// the latest log line, questions and errors, so the launcher needs no terminal.
//
// SPIKE: evaluating gogpu/ui (pure Go, no cgo on any platform).
package gui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

const (
	maxButtons   = 3
	windowWidth  = 560
	windowHeight = 180
)

var (
	accent     = widget.Hex(0xD97757) // Claude's orange
	background = widget.RGBA8(250, 249, 245, 255)
)

// Status is the handle the launcher's work uses to drive the window. All methods are
// safe to call from any goroutine: they only set gogpu/ui signals (thread-safe) and
// widget visibility (mutex-guarded).
//
// The window is one fixed view: headline, progress bar, detail line, and a row of up
// to three buttons that is hidden unless something is waiting for a click. It has to
// stay a tree of gogpu/ui's own widgets: the window composites from repaint-boundary
// layers rooted at its root widget, and a custom root widget renders nothing.
type Status struct {
	app *gogpu.App

	headline state.Signal[string]
	detail   state.Signal[string]
	progress state.Signal[float64]
	labels   [maxButtons]state.Signal[string]

	progressBox *primitives.BoxWidget
	buttonBoxes [maxButtons]*primitives.BoxWidget

	mu      sync.Mutex
	clicked chan int // receives the index of a clicked button; nil when none is shown

	// pinned stops log lines from overwriting the detail line while it's showing a
	// question or an error.
	pinned  atomic.Bool
	lastPct atomic.Int64
	closed  chan struct{} // closed when the window loop has ended
}

// Step sets the headline.
func (s *Status) Step(text string) {
	s.headline.Set(text)
	s.app.RequestRedraw()
}

// DownloadProgress reports download progress; it fits patcher.DownloadProgress.
func (s *Status) DownloadProgress(done, total int64) {
	if total <= 0 {
		return
	}
	pct := done * 100 / total
	if s.lastPct.Swap(pct) == pct {
		return // redraw only when the percentage changes
	}
	s.progress.Set(float64(done) / float64(total))
	if done >= total {
		s.Step("Installing Claude...")
	} else {
		s.Step(fmt.Sprintf("Downloading Claude... %d%% (%d / %d MB)", pct, done>>20, total>>20))
	}
}

// Ask shows a question with one button per option (up to three) and blocks until one
// is clicked, then restores the previous view. Returns the index clicked, or -1 if
// the window was closed instead.
func (s *Status) Ask(question, detail string, options []string) int {
	prevHeadline, prevDetail := s.headline.Get(), s.detail.Get()
	defer func() {
		s.progressBox.SetVisible(true)
		s.pinned.Store(false)
		s.headline.Set(prevHeadline)
		s.detail.Set(prevDetail)
		s.app.RequestRedraw()
	}()
	return s.prompt(question, detail, options)
}

// prompt replaces the progress bar with buttons, shows headline and detail, and
// waits for a click (or the window closing: -1). The caller restores the view.
func (s *Status) prompt(headline, detail string, options []string) int {
	if len(options) > maxButtons {
		options = options[:maxButtons]
	}
	clicked := make(chan int, 1)

	s.pinned.Store(true)
	s.progressBox.SetVisible(false)
	s.headline.Set(headline)
	s.detail.Set(detail)
	s.setButtons(options, clicked)
	defer s.setButtons(nil, nil)

	select {
	case i := <-clicked:
		return i
	case <-s.closed:
		return -1
	}
}

// setButtons shows one button per label, hiding the rest, and routes their clicks to
// clicked. setButtons(nil, nil) hides them all.
func (s *Status) setButtons(labels []string, clicked chan int) {
	s.mu.Lock()
	s.clicked = clicked
	s.mu.Unlock()
	for i := range s.buttonBoxes {
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		// Visibility first: the label change is what marks the view for redraw.
		s.buttonBoxes[i].SetVisible(label != "")
		s.labels[i].Set(label)
	}
	s.app.RequestRedraw()
}

func (s *Status) click(i int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clicked != nil {
		select {
		case s.clicked <- i:
		default: // already have a click
		}
	}
}

// Run shows the status window and runs work on another goroutine; call it from the
// main goroutine. Whatever work prints is mirrored to the detail line (and still
// reaches the original stdout, if there is one). On success the window closes
// itself; on error it shows the error until closed. If the window can't open at all
// (no display, no usable renderer), work simply finishes without it.
func Run(title string, work func(s *Status) error) error {
	gogpuApp := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle(title).
		WithSize(windowWidth, windowHeight).
		WithResizable(false))

	s := &Status{
		app:      gogpuApp,
		headline: state.NewSignal("Starting..."),
		detail:   state.NewSignal(""),
		progress: state.NewSignal(0.0),
		closed:   make(chan struct{}),
	}
	s.lastPct.Store(-1)
	for i := range s.labels {
		s.labels[i] = state.NewSignal("")
	}

	uiApp := app.New(
		app.WithWindowProvider(gogpuApp),
		app.WithPlatformProvider(gogpuApp),
		app.WithEventSource(gogpuApp.EventSource()),
		app.WithTheme(material3.New(accent).AsTheme()),
	)
	uiApp.SetRoot(s.build())
	s.setButtons(nil, nil)

	restore := mirrorOutput(s)
	defer restore()

	var result error
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result = work(s)
		switch {
		case result != nil:
			s.prompt("Something went wrong", result.Error(), []string{"Close"})
		case os.Getenv("CLAUDE_WEBEXT_GUI_HOLD") != "":
			// Testing aid: keep the finished window up for inspection.
			s.prompt(s.headline.Get(), "Done.", []string{"Finish"})
		}
		quit(gogpuApp, s.closed)
	}()

	if err := desktop.Run(gogpuApp, uiApp); err != nil {
		fmt.Fprintf(os.Stderr, "Status window unavailable (%v), continuing without it\n", err)
	}
	close(s.closed)
	<-finished // also covers the user closing the window while work is still running
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

func (s *Status) build() widget.Widget {
	// Hideable parts sit in their own Box: Box.Draw skips itself when hidden, but not
	// every widget checks its own visibility.
	s.progressBox = primitives.Box(
		progressbar.New(
			progressbar.ValueSignal(s.progress),
			progressbar.Height(8),
			progressbar.Radius(4),
			progressbar.ColorSchemeOpt(progressbar.ProgressBarColorScheme{
				Bar:   accent,
				Track: widget.Hex(0xE8E4DC),
			}),
		),
	).CrossAlign(primitives.CrossAxisStretch)

	buttons := make([]widget.Widget, maxButtons)
	for i := range buttons {
		i := i
		s.buttonBoxes[i] = primitives.Box(button.New(
			button.TextSignal(s.labels[i]),
			button.OnClick(func() { s.click(i) }),
		))
		buttons[i] = s.buttonBoxes[i]
	}

	// CrossAxisStretch (here and on progressBox) gives children the full width; the
	// progress bar would otherwise sit at its ~200px preferred width.
	return primitives.VBox(
		primitives.Text("").ContentSignal(s.headline).
			FontSize(18).
			Bold().
			Color(widget.RGBA8(33, 33, 33, 255)),
		s.progressBox,
		primitives.Text("").ContentSignal(s.detail).
			FontSize(12).
			Color(widget.RGBA8(100, 100, 100, 255)),
		primitives.HBox(buttons...).Gap(8),
	).
		CrossAlign(primitives.CrossAxisStretch).
		Padding(24).
		Gap(10).
		Background(background)
}

// mirrorOutput redirects stdout/stderr into the detail line, while still copying it
// to the original stdout. Returns a function that restores them.
func mirrorOutput(s *Status) func() {
	origOut, origErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	os.Stdout, os.Stderr = w, w

	done := make(chan struct{})
	go func() {
		defer close(done)
		// The copy to the original stdout ignores errors: a Windows GUI build has no
		// stdout, and a failing write must not stop this loop, or the pipe fills up
		// and every print in the launcher blocks.
		sc := bufio.NewScanner(io.TeeReader(r, ignoreErrors{origOut}))
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" && !s.pinned.Load() {
				s.detail.Set(truncate(line, 90))
				s.app.RequestRedraw()
			}
		}
		io.Copy(io.Discard, r) // an over-long line stops the scanner; keep draining
	}()

	return func() {
		os.Stdout, os.Stderr = origOut, origErr
		w.Close()
		<-done
		r.Close()
	}
}

type ignoreErrors struct{ w io.Writer }

func (i ignoreErrors) Write(p []byte) (int, error) {
	i.w.Write(p)
	return len(p), nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
