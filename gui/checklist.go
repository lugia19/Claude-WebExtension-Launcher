package gui

import (
	"fmt"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"claude-webext-patcher/status"
	"claude-webext-patcher/utils"
)

// Row is one line of the checklist.
type Row struct {
	ID       string
	Label    string // initial text; SetRow can change it
	Download bool   // the download progress bar sits under this row
}

type rowWidgets struct {
	icon    *widget.Icon
	spinner *widget.Activity // instead of the icon while the row is running
	label   *widget.Label
	note    *widget.Label
}

// Status is the handle the launcher's work uses to drive the window. All methods are
// safe from any goroutine: they change the widgets on the UI thread. Once the window
// has closed (or if it never opened) they do nothing.
type Status struct {
	w       *window // nil without a window
	logPath string

	rowOrder []Row
	rows     map[string]*rowWidgets

	progress     *widget.ProgressBar
	downloadNote *widget.Label // the Download row's note
	lastPct      int64

	title, detail *widget.Label
	buttons       *fyne.Container

	closed    chan struct{} // closed once the window is closing (or never opened)
	closeOnce sync.Once
}

func newStatus(rows []Row, logPath string) *Status {
	return &Status{
		logPath:  logPath,
		rowOrder: rows,
		rows:     map[string]*rowWidgets{},
		closed:   make(chan struct{}),
		lastPct:  -1,
	}
}

func (s *Status) isClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

// markClosed is called when the window starts closing, and again once the loop has
// ended.
func (s *Status) markClosed() {
	s.closeOnce.Do(func() { close(s.closed) })
}

// do runs fn on the UI thread, if the window is open.
func (s *Status) do(fn func()) {
	if s.w != nil {
		s.w.runOnUI(fn)
	}
}

// SetRow updates a checklist row. An empty label keeps the current one.
func (s *Status) SetRow(id, st, label, note string) {
	r := s.rows[id]
	if r == nil {
		return
	}
	s.do(func() {
		if st == status.Running {
			r.icon.Hide()
			r.spinner.Show()
			r.spinner.Start()
		} else {
			r.spinner.Stop()
			r.spinner.Hide()
			r.icon.SetResource(stateIcon(st))
			r.icon.Show()
		}
		if label != "" {
			r.label.SetText(label)
		}
		r.note.SetText(note)
	})
}

// stateIcon is the icon for a row state. (Fyne's icons need the app, so they can't be
// package variables.)
func stateIcon(st string) fyne.Resource {
	switch st {
	case status.Done:
		return theme.NewPrimaryThemedResource(theme.ConfirmIcon())
	case status.Skipped:
		return theme.NewDisabledResource(theme.ConfirmIcon())
	case status.Warning:
		return theme.NewWarningThemedResource(theme.WarningIcon())
	case status.Failed:
		return theme.NewErrorThemedResource(theme.CancelIcon())
	}
	return theme.NewDisabledResource(theme.RadioButtonIcon())
}

// DownloadProgress shows download progress under the download row; it fits
// patcher.DownloadProgress.
func (s *Status) DownloadProgress(done, total int64) {
	if total <= 0 {
		return
	}
	pct := done * 100 / total
	if pct == s.lastPct {
		return // update only when the percentage changes
	}
	s.lastPct = pct
	note := fmt.Sprintf("%d%%  (%d / %d MB)", pct, done>>20, total>>20)
	s.do(func() {
		s.progress.Show()
		s.progress.SetValue(float64(done) / float64(total))
		if s.downloadNote != nil {
			s.downloadNote.SetText(note)
		}
	})
}

// Ask shows a question with one button per option and blocks until one is clicked.
// Returns the index clicked, or -1 if the window was closed instead.
func (s *Status) Ask(question, detail string, options []string) int {
	s.setMessage(question, detail)
	defer s.setMessage("", "")
	return <-s.waitForClick(options)
}

func (s *Status) showError(err error) {
	s.showFinal("Something went wrong", err.Error())
}

// showDone shows msg as the run's end: see showFinal.
func (s *Status) showDone(msg string) {
	s.showFinal(msg, "")
}

// showFinal shows a message with Open logs and Close until the window is closed.
func (s *Status) showFinal(title, detail string) {
	s.setMessage(title, detail)
	for <-s.waitForClick([]string{"Open logs", "Close"}) == 0 {
		utils.OpenInViewer(s.logPath)
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
			s.showFinal("Claude is running.", "Close this window when you're done.")
			return
		}
	}
}

func (s *Status) setMessage(title, detail string) {
	s.do(func() {
		s.title.SetText(title)
		s.detail.SetText(detail)
	})
}

// waitForClick shows one button per label and returns a channel that receives the
// index clicked, or -1 once the window is closed. The buttons are removed again after
// a click.
func (s *Status) waitForClick(labels []string) <-chan int {
	clicked := make(chan int, 1)
	s.setButtons(labels, clicked)

	result := make(chan int, 1)
	go func() {
		select {
		case i := <-clicked:
			s.setButtons(nil, nil)
			result <- i
		case <-s.closed:
			result <- -1
		}
	}()
	return result
}

// setButtons shows one button per label, each sending its index to clicked.
func (s *Status) setButtons(labels []string, clicked chan int) {
	s.do(func() {
		objs := make([]fyne.CanvasObject, len(labels))
		for i, label := range labels {
			objs[i] = widget.NewButton(label, func() {
				select {
				case clicked <- i:
				default: // already have a click
				}
			})
		}
		// A row of three long labels is wider than the window: two per row then.
		if len(objs) > 2 {
			s.buttons.Layout = layout.NewGridLayoutWithColumns(2)
		} else {
			s.buttons.Layout = layout.NewHBoxLayout()
		}
		s.buttons.Objects = objs
		s.buttons.Refresh()
	})
}

// build makes the checklist screen. UI thread (or before the window loop).
func (s *Status) build() fyne.CanvasObject {
	s.progress = widget.NewProgressBar()
	s.progress.TextFormatter = func() string { return "" }
	s.progress.Hide()

	list := container.NewVBox()
	for _, row := range s.rowOrder {
		r := &rowWidgets{
			icon:    widget.NewIcon(stateIcon("")),
			spinner: widget.NewActivity(),
			label:   widget.NewLabel(row.Label),
			note:    widget.NewLabel(""),
		}
		r.spinner.Hide()
		r.note.Importance = widget.LowImportance
		s.rows[row.ID] = r
		iconSlot := container.NewGridWrap(fyne.NewSquareSize(20), container.NewStack(r.icon, r.spinner))
		list.Add(container.NewHBox(container.NewCenter(iconSlot), r.label, r.note))
		if row.Download {
			s.downloadNote = r.note
			list.Add(s.progress)
		}
	}

	s.title = heading("")
	s.detail = dim("")
	s.buttons = container.NewHBox()

	return screen(container.NewBorder(list, container.NewVBox(s.title, s.detail, s.buttons), nil, nil))
}
