package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Setup is a screen of checkboxes: the first-run setup (Run shows it before the
// checklist) and an instance's settings (from the instance list).
type Setup struct {
	Title    string
	Subtitle []string // one text line each
	Options  []SetupOption
	// Apply receives the final checkbox states and entry values (both in Options order;
	// "" for an option without an entry) once the screen is confirmed. It runs on a
	// background goroutine: for the first-run setup, before the launcher's work starts.
	Apply func(checked []bool, values []string)
	// Validate, if set, checks the options together once each entry is valid, and
	// returns why they can't be saved, or "". It runs on the UI thread.
	Validate func(checked []bool) string
	// Extra are more buttons, after the others.
	Extra []SetupButton
}

// SetupButton is an extra button on a setup screen.
type SetupButton struct {
	Label       string
	OnClick     func() // runs on the UI thread
	CloseWindow bool   // close the window after OnClick
}

// SetupOption is one checkbox on the setup screen.
type SetupOption struct {
	Label   string
	Checked bool // initial state
	Note    string // optional dim text under the checkbox
	// Entry is an optional text field for a value that goes with the checkbox (e.g. a
	// port). It's only editable, and only validated, while checked.
	Entry *SetupEntry
	// Advanced options are grouped at the end, in a section that starts collapsed
	// unless one of them is checked.
	Advanced bool
}

// SetupEntry is a SetupOption's text field: a short one after the checkbox, or with
// Lines, a multi-line one under it.
type SetupEntry struct {
	Value       string // initial text
	Width       float32
	Lines       int    // more than 0: multi-line, this many lines tall, full width
	Placeholder string
	// Validate returns why the text isn't acceptable, or "" if it is. It runs on the
	// UI thread.
	Validate func(text string) string
}

// heading is a screen's title.
func heading(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.TextStyle.Bold = true
	l.SizeName = theme.SizeNameSubHeadingText
	return l
}

// dim is secondary text.
func dim(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapWord
	return l
}

// errorLabel is text for an error, empty until set.
func errorLabel() *widget.Label {
	l := widget.NewLabel("")
	l.Importance = widget.DangerImportance
	l.Wrapping = fyne.TextWrapWord
	return l
}

// primaryButton is the screen's main action, in white; icon may be nil.
func primaryButton(label string, icon fyne.Resource, onClick func()) fyne.CanvasObject {
	b := widget.NewButtonWithIcon(label, icon, onClick)
	b.Importance = widget.HighImportance
	return primary(b)
}

// buildSetup returns a setup screen: the checkboxes, a confirm button that hands the
// final states and entry values to onConfirm, a Back button if onCancel isn't nil, and
// setup.Extra. They all run in the click handler, on the UI thread; onConfirm must
// switch screens. The options scroll if they don't fit; Advanced ones are in their own
// collapsible section, under a warning.
func (w *window) buildSetup(setup *Setup, confirm string, onConfirm func(checked []bool, values []string), onCancel func()) fyne.CanvasObject {
	content := container.NewVBox(heading(setup.Title))
	for _, line := range setup.Subtitle {
		content.Add(dim(line))
	}
	options := container.NewVBox()
	checks := make([]*widget.Check, len(setup.Options))
	entries := make([]*widget.Entry, len(setup.Options))
	errs := errorLabel()
	warning := errorLabel()
	warning.SetText("Don't turn these on unless you know what you're doing.")
	advanced, openAdvanced := container.NewVBox(warning), false
	for i, opt := range setup.Options {
		box := options
		if opt.Advanced {
			box = advanced
			openAdvanced = openAdvanced || opt.Checked
		}
		checks[i] = widget.NewCheck(opt.Label, nil)
		checks[i].SetChecked(opt.Checked)
		if opt.Entry == nil {
			box.Add(noFocusRing(checks[i]))
			if opt.Note != "" {
				box.Add(dim(opt.Note))
			}
			continue
		}
		entry := widget.NewEntry()
		if opt.Entry.Lines > 0 {
			entry = widget.NewMultiLineEntry()
			entry.SetMinRowsVisible(opt.Entry.Lines)
		}
		entry.SetPlaceHolder(opt.Entry.Placeholder)
		entry.SetText(opt.Entry.Value)
		entry.OnChanged = func(string) { errs.SetText("") }
		if !opt.Checked {
			entry.Disable()
		}
		checks[i].OnChanged = func(on bool) {
			if on {
				entry.Enable()
			} else {
				entry.Disable()
				errs.SetText("")
			}
		}
		entries[i] = entry
		if opt.Entry.Lines > 0 {
			box.Add(noFocusRing(checks[i]))
			if opt.Note != "" {
				box.Add(dim(opt.Note))
			}
			box.Add(entry)
			continue
		}
		field := container.NewGridWrap(fyne.NewSize(opt.Entry.Width, entry.MinSize().Height), entry)
		box.Add(container.NewHBox(noFocusRing(checks[i]), field))
		if opt.Note != "" {
			box.Add(dim(opt.Note))
		}
	}
	if len(advanced.Objects) > 1 { // more than the warning
		section := widget.NewAccordion(widget.NewAccordionItem("Advanced", advanced))
		if openAdvanced {
			section.Open(0)
		}
		options.Add(section)
	}
	options.Add(errs)

	sent := false // a quick double click can arrive before the screen has switched
	buttons := container.NewHBox(primaryButton(confirm, nil, func() {
		if sent {
			return
		}
		checked := make([]bool, len(checks))
		values := make([]string, len(checks))
		for i, c := range checks {
			checked[i] = c.Checked
			if entries[i] == nil {
				continue
			}
			values[i] = entries[i].Text
			if validate := setup.Options[i].Entry.Validate; c.Checked && validate != nil {
				if problem := validate(values[i]); problem != "" {
					errs.SetText(problem)
					return
				}
			}
		}
		if setup.Validate != nil {
			if problem := setup.Validate(checked); problem != "" {
				errs.SetText(problem)
				return
			}
		}
		sent = true
		onConfirm(checked, values)
	}))
	if onCancel != nil {
		buttons.Add(widget.NewButton("Back", onCancel))
	}
	for _, extra := range setup.Extra {
		var b *widget.Button
		b = widget.NewButton(extra.Label, func() {
			if extra.CloseWindow {
				b.Disable() // once: e.g. Uninstall… starts another process
			}
			if extra.OnClick != nil {
				extra.OnClick()
			}
			if extra.CloseWindow {
				w.quit()
			}
		})
		buttons.Add(b)
	}
	return screen(container.NewBorder(content, buttons, nil, nil, container.NewVScroll(options)))
}
