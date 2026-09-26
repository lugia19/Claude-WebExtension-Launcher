package gui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Instances is what the instance list needs from the launcher. The callbacks run on
// the UI thread unless noted, so they should be quick.
type Instances struct {
	// Enabled reports whether to show the list. It's asked once the work is done,
	// since the first-run setup can turn the list on.
	Enabled func() bool
	List    func() []Instance
	// Launch starts an instance. It runs on a background goroutine.
	Launch func(name string) error
	// Validate returns why name can't be added, or "" if it can.
	Validate func(name string) string
	Add      func(name string) error
	// Running reports whether an instance is open (it can't be deleted then).
	Running func(name string) bool
	Delete  func(name string) error
	// Settings returns an instance's settings screen, or nil if there's nothing to set
	// (no Settings button then). Its Apply runs on a background goroutine.
	Settings func(name string) *Setup
	// LauncherSettings returns the launcher's own settings screen (Launcher settings,
	// at the bottom of the list). Its Apply runs on a background goroutine.
	LauncherSettings func() *Setup
}

// Instance is one row of the list.
type Instance struct {
	Name      string // what Launch/Delete/Settings take
	Display   string
	Deletable bool
}

const maxNameLength = 32

// showList switches to the instance list. UI thread only.
func (w *window) showList() {
	w.notes = map[string]*widget.Label{}
	rows := container.NewVBox()
	for _, inst := range w.inst.List() {
		rows.Add(w.listRow(inst))
	}

	settings := widget.NewButtonWithIcon("Launcher settings", theme.SettingsIcon(), w.showLauncherSettings)
	gutter := theme.Size(theme.SizeNameScrollBar) // keeps the rows clear of the scrollbar
	w.win.SetContent(screen(container.NewBorder(
		heading("Instances"),
		container.NewHBox(widget.NewButtonWithIcon("Add instance", theme.ContentAddIcon(), w.showAdd), layout.NewSpacer(), settings),
		nil, nil,
		container.NewVScroll(container.New(layout.NewCustomPaddedLayout(0, 0, 0, gutter), rows)),
	)))
}

func (w *window) listRow(inst Instance) fyne.CanvasObject {
	name := widget.NewLabel(inst.Display)
	name.TextStyle.Bold = true
	w.mu.Lock()
	note := widget.NewLabel(w.note[inst.Name])
	w.mu.Unlock()
	w.notes[inst.Name] = note
	note.Importance = widget.LowImportance
	note.SizeName = theme.SizeNameCaptionText
	note.Truncation = fyne.TextTruncateEllipsis
	note.Hidden = note.Text == "" // so the name is centered next to the buttons

	// Launch is always last, so it lines up on the right whatever else a row has.
	buttons := container.NewHBox()
	if w.inst.Settings != nil && w.inst.Settings(inst.Name) != nil {
		b := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() { w.showSettings(inst) })
		b.Importance = widget.LowImportance
		buttons.Add(b)
	}
	if inst.Deletable {
		b := widget.NewButtonWithIcon("", theme.NewErrorThemedResource(theme.DeleteIcon()), func() { w.showDelete(inst) })
		b.Importance = widget.LowImportance
		buttons.Add(b)
	}
	buttons.Add(primaryButton("Launch", theme.MediaPlayIcon(), func() { w.launch(inst.Name) }))

	// Labels pad their text, which on top of the box's gap would space the name and its
	// note far apart: overlap that padding instead.
	label := container.New(layout.NewCustomPaddedVBoxLayout(-theme.Padding()-theme.InnerPadding()), name, note)
	return container.NewBorder(nil, nil, nil, container.NewCenter(buttons), container.NewVBox(layout.NewSpacer(), label, layout.NewSpacer()))
}

// setNote sets an instance's note in the list, keeping it across list rebuilds.
func (w *window) setNote(name, v string) {
	w.mu.Lock()
	w.note[name] = v
	w.mu.Unlock()
	w.runOnUI(func() {
		if l := w.notes[name]; l != nil {
			l.SetText(v)
			if v == "" {
				l.Hide()
			} else {
				l.Show()
			}
		}
	})
}

func (w *window) launch(name string) {
	w.setNote(name, "Starting…")
	w.mu.Lock()
	w.launching[name]++ // counted: Launch can be clicked again before the first finishes
	w.mu.Unlock()
	w.background(func() {
		defer func() {
			w.mu.Lock()
			if w.launching[name]--; w.launching[name] <= 0 {
				delete(w.launching, name)
			}
			w.mu.Unlock()
		}()
		if err := w.inst.Launch(name); err != nil {
			w.setNote(name, "Couldn't start: "+err.Error())
			return
		}
		w.setNote(name, "Started")
	})
}

// showAdd switches to the add-an-instance screen. UI thread only.
func (w *window) showAdd() {
	problem := errorLabel()
	field := widget.NewEntry()
	field.SetPlaceHolder("Name, e.g. work")
	submit := func() {
		name := strings.TrimSpace(field.Text)
		if msg := w.inst.Validate(name); msg != "" {
			problem.SetText(msg)
			return
		}
		if err := w.inst.Add(name); err != nil {
			problem.SetText(err.Error())
			return
		}
		w.showList()
	}
	field.OnSubmitted = func(string) { submit() }
	field.OnChanged = func(v string) {
		if r := []rune(v); len(r) > maxNameLength {
			field.SetText(string(r[:maxNameLength]))
		}
		problem.SetText("")
	}

	w.win.SetContent(screen(container.NewBorder(
		container.NewVBox(
			heading("Add an instance"),
			dim("Each instance has its own login, settings and sessions."),
			field,
			problem,
		),
		container.NewHBox(primaryButton("Add", nil, submit), widget.NewButton("Cancel", w.showList)),
		nil, nil,
	)))
	w.win.Canvas().Focus(field)
}

// showDelete switches to the delete confirmation for inst. UI thread only.
func (w *window) showDelete(inst Instance) {
	content := container.NewVBox(heading("Delete " + inst.Display + "?"))
	var buttons *fyne.Container
	w.mu.Lock()
	starting := w.launching[inst.Name] > 0 // not holding its lock yet, so Running can't tell
	w.mu.Unlock()
	if starting || w.inst.Running(inst.Name) {
		content.Add(dim("It's open right now. Close it first, then try again."))
		buttons = container.NewHBox(widget.NewButton("Back", w.showList))
	} else {
		problem := errorLabel()
		content.Add(dim("This removes its data folder (its login, settings and local sessions) and its menu and startup shortcuts. It can't be undone."))
		content.Add(problem)

		// Both buttons lock once the delete starts: going back to the list meanwhile would
		// allow launching the instance while its folder is being removed.
		var del, cancel *widget.Button
		del = widget.NewButtonWithIcon("Delete", theme.DeleteIcon(), func() {
			del.Disable()
			cancel.Disable()
			problem.SetText("Deleting…")
			// A big data folder takes a while to remove: keep the UI thread free.
			w.background(func() {
				err := w.inst.Delete(inst.Name)
				w.runOnUI(func() {
					if err != nil {
						del.Enable()
						cancel.Enable()
						problem.SetText(err.Error())
						return
					}
					w.showList()
				})
			})
		})
		del.Importance = widget.DangerImportance
		cancel = widget.NewButton("Cancel", w.showList)
		buttons = container.NewHBox(del, cancel)
	}
	w.win.SetContent(screen(container.NewBorder(content, buttons, nil, nil)))
}

// showSettings switches to inst's settings. Saving returns to the list right away and
// applies them in the background (creating shortcuts can take a moment), with the
// row's note saying how it went.
func (w *window) showSettings(inst Instance) {
	setup := w.inst.Settings(inst.Name)
	if setup == nil {
		return
	}
	w.win.SetContent(w.buildSetup(setup, "Save", func(checked []bool) {
		w.setNote(inst.Name, "Saving settings…")
		w.showList()
		w.background(func() {
			setup.Apply(checked)
			w.setNote(inst.Name, "Settings saved")
		})
	}, w.showList))
}

// showLauncherSettings switches to the launcher's settings; saving returns to the list
// and applies them in the background.
func (w *window) showLauncherSettings() {
	setup := w.inst.LauncherSettings()
	w.win.SetContent(w.buildSetup(setup, "Save", func(checked []bool) {
		w.showList()
		w.background(func() { setup.Apply(checked) })
	}, w.showList))
}
