package gui

import (
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/core/checkbox"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/widget"
)

// Setup is the first-run screen: a few checkboxes and Continue. Run shows it before
// the checklist when given one.
type Setup struct {
	Title    string
	Subtitle []string // one text line each
	Options  []SetupOption
	// Apply receives the final checkbox states (in Options order) once Continue is
	// clicked. It runs on the work goroutine, before the launcher's work starts.
	Apply func(checked []bool)
}

// SetupOption is one checkbox on the setup screen.
type SetupOption struct {
	Label   string
	Checked bool // initial state
}

// buildSetup returns the setup screen. Continue hands the choices to done and swaps
// the window over to the checklist. The swap happens in the click handler, which
// runs on the UI thread: gogpu/ui has no way to post work to that thread, and
// replacing the root from any other goroutine isn't safe.
func buildSetup(setup *Setup, uiApp *app.App, checklist widget.Widget, done chan<- []bool) widget.Widget {
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
	children = append(children, primitives.HBox(button.New(
		button.TextOpt("Continue"),
		button.BackgroundOpt(widget.Hex(0xFFFFFF)), // the theme's label color is already dark
		button.OnClick(func() {
			if sent {
				return
			}
			sent = true
			done <- append([]bool(nil), checked...)
			uiApp.SetRoot(checklist)
		}),
	)))

	return primitives.VBox(children...).
		Gap(14).
		CrossAlign(primitives.CrossAxisStretch).
		Padding(24).
		Background(background)
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
