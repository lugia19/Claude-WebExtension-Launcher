package gui

import (
	"strings"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/scrollview"
	"github.com/gogpu/ui/core/textfield"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/widget"
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
	// Headless is the instance launched when the window can't open.
	Headless string
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
	rows := []widget.Widget{}
	w.mu.Lock()
	w.notes = map[string]*text{}
	w.mu.Unlock()
	for _, inst := range w.inst.List() {
		rows = append(rows, w.listRow(inst))
	}

	title := primitives.Text("Instances").FontSize(18).Bold().Color(textColor)
	list := scrollview.New(primitives.VBox(rows...).Gap(14).CrossAlign(primitives.CrossAxisStretch))
	settings := button.New(
		button.TextOpt("Launcher settings"),
		button.PainterOpt(iconPainter{icon: cogIcon, fg: textColor, bg: &trackColor}),
		button.OnClick(w.showLauncherSettings),
	).MinWidth(180)
	w.uiApp.SetRoot(primitives.VBox(
		title,
		primitives.Expanded(list),
		primitives.HBox(
			primitives.Expanded(primitives.HBox(button.New(button.TextOpt("Add instance"), button.OnClick(w.showAdd)))),
			settings,
		),
	).
		CrossAlign(primitives.CrossAxisStretch).
		Padding(24).
		Gap(16).
		Background(background))
}

func (w *window) listRow(inst Instance) widget.Widget {
	name := primitives.Text(inst.Display).FontSize(14).Color(textColor)
	w.mu.Lock()
	note := w.s.newText(w.note[inst.Name], 12, dimColor)
	w.notes[inst.Name] = note
	w.mu.Unlock()
	note.widget.MaxLines(1).Ellipsis()

	// Launch is always last, so it lines up on the right whatever else a row has.
	buttons := []widget.Widget{}
	if w.inst.Settings != nil && w.inst.Settings(inst.Name) != nil {
		buttons = append(buttons, iconButton(cogIcon, dimColor, func() { w.showSettings(inst) }))
	}
	if inst.Deletable {
		buttons = append(buttons, iconButton(trashIcon, errorColor, func() { w.showDelete(inst) }))
	}
	white := widget.Hex(0xFFFFFF)
	buttons = append(buttons, button.New(
		button.TextOpt("Launch"),
		button.SizeOpt(button.Small),
		button.PainterOpt(iconPainter{icon: playIcon, fg: background, bg: &white}),
		button.OnClick(func() { w.launch(inst.Name) }),
	).MinWidth(92))

	// HBox top-aligns its children, so the name block is only as tall as the buttons.
	label := primitives.VBox(name, note.widget).Gap(2)
	return primitives.HBox(append([]widget.Widget{primitives.Expanded(label)}, buttons...)...).Gap(8)
}

// setNote sets an instance's note in the list, keeping it across list rebuilds.
func (w *window) setNote(name, v string) {
	w.mu.Lock()
	if w.note == nil {
		w.note = map[string]string{}
	}
	w.note[name] = v
	t := w.notes[name]
	w.mu.Unlock()
	if t != nil {
		t.Set(v)
		w.s.redraw()
	}
}

func (w *window) launch(name string) {
	w.setNote(name, "Starting…")
	go func() {
		if err := w.inst.Launch(name); err != nil {
			w.setNote(name, "Couldn't start: "+err.Error())
			return
		}
		w.setNote(name, "Started")
	}()
}

// showAdd switches to the add-an-instance screen. UI thread only.
func (w *window) showAdd() {
	value := state.NewSignal("")
	problem := w.s.newText("", 13, errorColor)
	problem.widget.MaxLines(2).Ellipsis()

	submit := func() {
		name := strings.TrimSpace(value.Get())
		if msg := w.inst.Validate(name); msg != "" {
			problem.Set(msg)
			w.s.redraw()
			return
		}
		if err := w.inst.Add(name); err != nil {
			problem.Set(err.Error())
			w.s.redraw()
			return
		}
		w.showList()
	}
	field := textfield.New(
		textfield.ValueSignal(value),
		textfield.Placeholder("Name, e.g. work"),
		textfield.MaxLength(maxNameLength),
		textfield.OnSubmit(func(string) { submit() }),
		textfield.OnChange(func(string) { problem.Set("") }),
		textfield.PainterOpt(themedTextField{}),
	)

	w.uiApp.SetRoot(primitives.VBox(
		primitives.Text("Add an instance").FontSize(18).Bold().Color(textColor),
		primitives.Text("Each instance has its own login, settings and sessions.").FontSize(13).Color(dimColor),
		field,
		problem.widget,
		primitives.HBox(primaryButton("Add", submit), button.New(button.TextOpt("Cancel"), button.OnClick(w.showList))).Gap(8),
	).
		CrossAlign(primitives.CrossAxisStretch).
		Padding(24).
		Gap(14).
		Background(background))
	w.uiApp.Window().Context().RequestFocus(field)
}

// showDelete switches to the delete confirmation for inst. UI thread only.
func (w *window) showDelete(inst Instance) {
	children := []widget.Widget{
		primitives.Text("Delete " + inst.Display + "?").FontSize(18).Bold().Color(textColor),
	}
	if w.inst.Running(inst.Name) {
		children = append(children,
			primitives.Text("It's open right now. Close it first, then try again.").FontSize(13).Color(dimColor),
			primitives.HBox(button.New(button.TextOpt("Back"), button.OnClick(w.showList))),
		)
	} else {
		problem := w.s.newText("", 13, errorColor)
		problem.widget.MaxLines(3).Ellipsis()
		deleting := false
		children = append(children,
			primitives.Text("This removes its data folder (its login, settings and local sessions)").FontSize(13).Color(dimColor),
			primitives.Text("and its menu and startup shortcuts. It can't be undone.").FontSize(13).Color(dimColor),
			problem.widget,
			primitives.HBox(
				button.New(button.TextOpt("Delete"), button.BackgroundOpt(errorColor), button.OnClick(func() {
					if deleting {
						return
					}
					deleting = true
					problem.Set("Deleting…")
					w.s.redraw()
					// A big data folder takes a while to remove: keep the UI thread free.
					go func() {
						err := w.inst.Delete(inst.Name)
						w.runOnUI(func() {
							if err != nil {
								deleting = false
								problem.Set(err.Error())
								return
							}
							w.showList()
						})
					}()
				})),
				button.New(button.TextOpt("Cancel"), button.OnClick(w.showList)),
			).Gap(8),
		)
	}
	w.uiApp.SetRoot(primitives.VBox(children...).
		CrossAlign(primitives.CrossAxisStretch).
		Padding(24).
		Gap(14).
		Background(background))
}

// showSettings switches to inst's settings. Saving returns to the list right away and
// applies them in the background (creating shortcuts can take a moment), with the
// row's note saying how it went.
func (w *window) showSettings(inst Instance) {
	setup := w.inst.Settings(inst.Name)
	if setup == nil {
		return
	}
	w.uiApp.SetRoot(buildSetup(setup, "Save", func(checked []bool) {
		w.setNote(inst.Name, "Saving settings…")
		w.showList()
		go func() {
			setup.Apply(checked)
			w.setNote(inst.Name, "Settings saved")
		}()
	}, w.showList))
}

// showLauncherSettings switches to the launcher's settings; saving returns to the list
// and applies them in the background.
func (w *window) showLauncherSettings() {
	setup := w.inst.LauncherSettings()
	w.uiApp.SetRoot(buildSetup(setup, "Save", func(checked []bool) {
		w.showList()
		go setup.Apply(checked)
	}, w.showList))
}

// themedTextField draws text fields in the window's colors, like themedCheckbox.
type themedTextField struct{ textfield.DefaultPainter }

func (p themedTextField) PaintTextField(canvas widget.Canvas, st *textfield.PaintState) {
	st.ColorScheme = textfield.TextFieldColorScheme{
		Background:  trackColor,
		Border:      dimColor,
		FocusBorder: accent,
		ErrorBorder: errorColor,
		TextColor:   textColor,
		Placeholder: dimColor,
		CursorColor: textColor,
		DisabledBg:  trackColor,
		DisabledFg:  dimColor,
		SelectionBg: accent,
		ErrorText:   errorColor,
	}
	p.DefaultPainter.PaintTextField(canvas, st)
}
