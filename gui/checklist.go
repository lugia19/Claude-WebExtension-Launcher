package gui

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/progressbar"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"

	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
)

// Dark theme, in Claude's colors.
var (
	background = widget.Hex(0x262624)
	textColor  = widget.Hex(0xF5F4EE)
	dimColor   = widget.Hex(0x9B9A94)
	accent     = widget.Hex(0xD97757)
	warnColor  = widget.Hex(0xE0B340)
	errorColor = widget.Hex(0xE5484D)
	trackColor = widget.Hex(0x3A3936)
)

const maxButtons = 3

// Row is one line of the checklist.
type Row struct {
	ID       string
	Label    string // initial text; SetRow can change it
	Download bool   // the download progress bar sits under this row
}

// text is a text widget bound to a signal. Set must be used to change it: gogpu/ui
// only repaints a text when its signal changes, without laying it out again, so a
// text whose width changes (e.g. from empty) would keep its old size — invisible,
// or cut off.
type text struct {
	sig    state.Signal[string]
	widget *primitives.TextWidget
	s      *Status
}

func (s *Status) newText(initial string, size float32, color widget.Color) *text {
	t := &text{sig: state.NewSignal(initial), s: s}
	t.widget = primitives.Text("").ContentSignal(t.sig).FontSize(size).Color(color)
	return t
}

func (t *text) Set(v string) {
	if t.sig.Get() == v {
		return
	}
	t.sig.Set(v)
	if !t.s.isClosed() {
		t.widget.MarkNeedsLayout()
	}
}

type rowWidgets struct {
	// One icon text per color: text color is fixed at construction, so a row switches
	// color by giving the glyph to a different text. The others are empty.
	accentIcon, dimIcon, warnIcon, errorIcon *text
	label, note                              *text
}

// Status is the handle the launcher's work uses to drive the window. All methods are
// safe from any goroutine: they only set gogpu/ui signals (thread-safe), widget
// visibility and layout flags (mutex-guarded). Once the window has closed they
// become no-ops, since the app they'd poke at is gone.
//
// The window is one fixed tree of gogpu/ui widgets: the checklist, then a message
// area (title, detail, up to three buttons). It must stay that way: the window
// composites from repaint-boundary layers rooted at its root widget, and a custom
// root widget renders nothing. Parts are hidden by wrapping them in a Box and toggling
// its visibility (Box.Draw skips itself when hidden; not every widget does).
type Status struct {
	app     *gogpu.App
	logPath string

	rowOrder []Row
	rows     map[string]*rowWidgets

	progress    state.Signal[float64]
	progressBox *primitives.BoxWidget
	lastPct     int64

	title, detail *text
	labels        [maxButtons]state.Signal[string]
	buttonBoxes   [maxButtons]*primitives.BoxWidget

	mu      sync.Mutex
	clicked chan int // receives the index of a clicked button; nil when none is shown

	closedFlag atomic.Bool
	closed     chan struct{} // closed when the window loop has ended
}

func newStatus(a *gogpu.App, rows []Row, logPath string) *Status {
	s := &Status{
		app:      a,
		logPath:  logPath,
		rowOrder: rows,
		rows:     map[string]*rowWidgets{},
		progress: state.NewSignal(0.0),
		closed:   make(chan struct{}),
		lastPct:  -1,
	}
	s.title = s.newText("", 16, textColor)
	s.title.widget.Bold()
	s.detail = s.newText("", 13, dimColor)
	s.detail.widget.MaxLines(3).Ellipsis()
	for _, r := range rows {
		s.rows[r.ID] = &rowWidgets{
			accentIcon: s.newText("", 14, accent),
			dimIcon:    s.newText("○", 14, dimColor),
			warnIcon:   s.newText("", 14, warnColor),
			errorIcon:  s.newText("", 14, errorColor),
			label:      s.newText(r.Label, 14, textColor),
			note:       s.newText("", 13, dimColor),
		}
	}
	for i := range s.labels {
		s.labels[i] = state.NewSignal("")
	}
	return s
}

func (s *Status) isClosed() bool {
	return s.closedFlag.Load()
}

// markClosed is called once the window loop has ended.
func (s *Status) markClosed() {
	s.closedFlag.Store(true)
	close(s.closed)
}

func (s *Status) redraw() {
	if !s.isClosed() {
		s.app.RequestRedraw()
	}
}

// SetRow updates a checklist row. An empty label keeps the current one.
func (s *Status) SetRow(id, st, label, note string) {
	r := s.rows[id]
	if r == nil {
		return
	}
	glyph, color := icon(st)
	show := func(t *text, c string) {
		if c == color {
			t.Set(glyph)
		} else {
			t.Set("")
		}
	}
	show(r.accentIcon, "accent")
	show(r.dimIcon, "dim")
	show(r.warnIcon, "warn")
	show(r.errorIcon, "error")
	if label != "" {
		r.label.Set(label)
	}
	r.note.Set(note)
	s.redraw()
}

func icon(st string) (glyph, color string) {
	switch st {
	case status.Running:
		return "●", "accent"
	case status.Done:
		return "✓", "accent"
	case status.Skipped:
		return "✓", "dim"
	case status.Warning:
		return "⚠", "warn"
	case status.Failed:
		return "✗", "error"
	}
	return "○", "dim"
}

// DownloadProgress shows download progress under the download row; it fits
// patcher.DownloadProgress.
func (s *Status) DownloadProgress(done, total int64) {
	if total <= 0 {
		return
	}
	pct := done * 100 / total
	if pct == s.lastPct {
		return // redraw only when the percentage changes
	}
	s.lastPct = pct
	s.progressBox.SetVisible(true)
	s.progress.Set(float64(done) / float64(total))
	for _, row := range s.rowOrder {
		if row.Download {
			s.rows[row.ID].note.Set(fmt.Sprintf("%d%%  (%d / %d MB)", pct, done>>20, total>>20))
		}
	}
	s.redraw()
}

// Ask shows a question with one button per option (up to three) and blocks until one
// is clicked. Returns the index clicked, or -1 if the window was closed instead.
func (s *Status) Ask(question, detail string, options []string) int {
	s.setMessage(question, detail)
	defer s.setMessage("", "")
	return <-s.waitForClick(options)
}

func (s *Status) showError(err error) {
	s.setMessage("Something went wrong", err.Error())
	for {
		switch <-s.waitForClick([]string{"Open logs", "Close"}) {
		case 0:
			utils.OpenInViewer(s.logPath)
		default: // Close, or the window was closed
			return
		}
	}
}

// countDown gives the user a few seconds to open the log before the window closes.
// Opening the log stops the countdown.
func (s *Status) countDown(seconds int) {
	clicks := s.waitForClick([]string{"Open logs", "Close"})
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for left := seconds; left > 0; {
		s.setMessage("Claude is starting.", fmt.Sprintf("Closing in %d…", left))
		select {
		case <-tick.C:
			left--
		case i := <-clicks:
			if i != 0 {
				return
			}
			utils.OpenInViewer(s.logPath)
			s.setMessage("Claude is running.", "Close this window when you're done.")
			for <-s.waitForClick([]string{"Open logs", "Close"}) == 0 {
				utils.OpenInViewer(s.logPath)
			}
			return
		}
	}
}

func (s *Status) setMessage(title, detail string) {
	s.title.Set(title)
	s.detail.Set(detail)
	s.redraw()
}

// waitForClick shows one button per label and returns a channel that receives the
// index clicked, or -1 once the window is closed. The buttons are hidden again after
// a click.
func (s *Status) waitForClick(labels []string) <-chan int {
	if len(labels) > maxButtons {
		labels = labels[:maxButtons]
	}
	clicked := make(chan int, 1)
	s.setButtons(labels, clicked)

	result := make(chan int, 1)
	go func() {
		select {
		case i := <-clicked:
			s.hideButtons()
			result <- i
		case <-s.closed:
			result <- -1
		}
	}()
	return result
}

func (s *Status) hideButtons() {
	s.setButtons(nil, nil)
}

// setButtons shows one button per label, hiding the rest, and routes their clicks to
// clicked.
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
	s.redraw()
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

func (s *Status) build() widget.Widget {
	s.progressBox = primitives.Box(
		progressbar.New(
			progressbar.ValueSignal(s.progress),
			progressbar.Height(6),
			progressbar.Radius(3),
			progressbar.ColorSchemeOpt(progressbar.ProgressBarColorScheme{Bar: accent, Track: trackColor}),
		),
	).CrossAlign(primitives.CrossAxisStretch).Padding(4)
	s.progressBox.SetVisible(false)

	checklist := make([]widget.Widget, 0, len(s.rowOrder)+1)
	for _, row := range s.rowOrder {
		r := s.rows[row.ID]
		iconSlot := primitives.Box(primitives.HBox(
			r.accentIcon.widget, r.dimIcon.widget, r.warnIcon.widget, r.errorIcon.widget,
		)).Width(20)
		checklist = append(checklist, primitives.HBox(iconSlot, r.label.widget, r.note.widget).Gap(8))
		if row.Download {
			checklist = append(checklist, s.progressBox)
		}
	}

	buttons := make([]widget.Widget, maxButtons)
	for i := range buttons {
		i := i
		s.buttonBoxes[i] = primitives.Box(button.New(
			button.TextSignal(s.labels[i]),
			button.OnClick(func() { s.click(i) }),
		))
		buttons[i] = s.buttonBoxes[i]
	}

	// CrossAxisStretch gives children the full width; the progress bar would otherwise
	// sit at its ~200px preferred width.
	return primitives.VBox(
		primitives.VBox(checklist...).Gap(10).CrossAlign(primitives.CrossAxisStretch),
		primitives.VBox(
			s.title.widget,
			s.detail.widget,
			primitives.HBox(buttons...).Gap(8),
		).Gap(8).CrossAlign(primitives.CrossAxisStretch),
	).
		CrossAlign(primitives.CrossAxisStretch).
		Padding(24).
		Gap(24).
		Background(background)
}
