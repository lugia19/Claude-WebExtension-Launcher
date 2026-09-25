package gui

import (
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
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

// buildSetup returns a setup screen: the checkboxes, a confirm button that hands the
// final states to onConfirm, a Back button if onCancel isn't nil, and setup.Extra.
// They all run in the click handler, on the UI thread, so they may switch screens.
func (w *window) buildSetup(setup *Setup, confirm string, onConfirm func(checked []bool), onCancel func()) widget.Widget {
	checked := make([]bool, len(setup.Options))
	subtitle := make([]widget.Widget, len(setup.Subtitle))
	for i, line := range setup.Subtitle {
		subtitle[i] = primitives.Text(line).FontSize(13).Color(dimColor)
	}
	children := []widget.Widget{
		primitives.Text(setup.Title).FontSize(18).Bold().Color(textColor),
		primitives.VBox(subtitle...).Gap(4),
	}
	for i, opt := range setup.Options {
		i := i
		checked[i] = opt.Checked
		children = append(children, checkbox.New(
			checkbox.LabelOpt(opt.Label),
			checkbox.Checked(opt.Checked),
			checkbox.OnToggle(func(on bool) { checked[i] = on }),
			checkbox.PainterOpt(themedCheckbox{}),
		))
	}

	sent := false
	buttons := []widget.Widget{primaryButton(confirm, func() {
		if sent {
			return
		}
		sent = true
		onConfirm(append([]bool(nil), checked...))
	})}
	if onCancel != nil {
		buttons = append(buttons, button.New(button.TextOpt("Back"), button.OnClick(onCancel)))
	}
	for _, extra := range setup.Extra {
		extra := extra
		buttons = append(buttons, button.New(button.TextOpt(extra.Label), button.OnClick(func() {
			if extra.OnClick != nil {
				extra.OnClick()
			}
			if extra.CloseWindow {
				w.gogpuApp.Quit()
			}
		})))
	}
	children = append(children, primitives.HBox(buttons...).Gap(8))

	return primitives.VBox(children...).
		Gap(14).
		CrossAlign(primitives.CrossAxisStretch).
		Padding(24).
		Background(background)
}

// primaryButton is the screen's main action: white, since the default painter's
// label color is already dark (the other buttons keep its grey).
func primaryButton(label string, onClick func()) widget.Widget {
	return button.New(
		button.TextOpt(label),
		button.BackgroundOpt(widget.Hex(0xFFFFFF)),
		button.OnClick(onClick),
	)
}

// themedCheckbox draws checkboxes in the window's colors. The checkbox widget only
// gets colors from a theme painter (otherwise it's purple with black labels, which
// vanish on the dark background), so fill the scheme in and use the default drawing.
type themedCheckbox struct{ checkbox.DefaultPainter }

func (p themedCheckbox) PaintCheckbox(canvas widget.Canvas, state checkbox.PaintState) {
	state.ColorScheme = checkbox.CheckboxColorScheme{
		CheckedBg:       accent,
		CheckedFg:       textColor,
		UncheckedBorder: dimColor,
		LabelColor:      textColor,
		DisabledBg:      trackColor,
		DisabledFg:      dimColor,
		FocusRing:       accent,
	}
	p.DefaultPainter.PaintCheckbox(canvas, state)
}
