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
	// Apply receives the final checkbox states (in Options order) once the screen is
	// confirmed. It runs on a background goroutine: for the first-run setup, before the
	// launcher's work starts.
	Apply func(checked []bool)
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
// final states to onConfirm, a Back button if onCancel isn't nil, and setup.Extra.
// They all run in the click handler, on the UI thread; onConfirm must switch screens.
func (w *window) buildSetup(setup *Setup, confirm string, onConfirm func(checked []bool), onCancel func()) fyne.CanvasObject {
	content := container.NewVBox(heading(setup.Title))
	for _, line := range setup.Subtitle {
		content.Add(dim(line))
	}
	checks := make([]*widget.Check, len(setup.Options))
	for i, opt := range setup.Options {
		checks[i] = widget.NewCheck(opt.Label, nil)
		checks[i].SetChecked(opt.Checked)
		content.Add(noFocusRing(checks[i]))
	}

	sent := false // a quick double click can arrive before the screen has switched
	buttons := container.NewHBox(primaryButton(confirm, nil, func() {
		if sent {
			return
		}
		sent = true
		checked := make([]bool, len(checks))
		for i, c := range checks {
			checked[i] = c.Checked
		}
		onConfirm(checked)
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
	return screen(container.NewBorder(content, buttons, nil, nil))
}
