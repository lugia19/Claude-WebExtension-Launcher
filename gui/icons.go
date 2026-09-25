package gui

import (
	"math"

	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// The few icons the window uses, drawn from plain shapes (circles, rounded rects,
// lines). gogpu/ui's own icons fill SVG paths, and on Linux (GLES) that breaks the
// rest of the frame: everything else on the screen stops being painted.
type shape int

const (
	cogIcon shape = iota
	trashIcon
	playIcon
)

// draw draws the icon in r (square) in color fg, on background bg (for the cog's hole).
func (i shape) draw(canvas widget.Canvas, r geometry.Rect, fg, bg widget.Color) {
	x, y, s := r.Min.X, r.Min.Y, r.Width()
	c := r.Center()
	switch i {
	case cogIcon:
		for k := 0; k < 8; k++ {
			a := float64(k) * math.Pi / 4
			dx, dy := float32(math.Cos(a)), float32(math.Sin(a))
			canvas.DrawLine(
				geometry.Pt(c.X+dx*s*0.25, c.Y+dy*s*0.25),
				geometry.Pt(c.X+dx*s*0.48, c.Y+dy*s*0.48),
				fg, s*0.16)
		}
		canvas.DrawCircle(c, s*0.34, fg)
		canvas.DrawCircle(c, s*0.14, bg)
	case trashIcon:
		canvas.DrawRoundRect(geometry.NewRect(x+s*0.38, y+s*0.08, s*0.24, s*0.12), fg, s*0.03) // handle
		canvas.DrawRoundRect(geometry.NewRect(x+s*0.14, y+s*0.18, s*0.72, s*0.1), fg, s*0.03)  // lid
		canvas.DrawRoundRect(geometry.NewRect(x+s*0.22, y+s*0.33, s*0.56, s*0.6), fg, s*0.08)  // body
	case playIcon:
		// A filled triangle, as vertical strokes narrowing toward the tip.
		left, tip, half := x+s*0.24, x+s*0.84, s*0.36
		const steps = 12
		w := (tip - left) / steps
		for k := 0; k < steps; k++ {
			px := left + (float32(k)+0.5)*w
			h := half * (1 - (float32(k)+0.5)/steps)
			canvas.DrawLine(geometry.Pt(px, c.Y-h), geometry.Pt(px, c.Y+h), fg, w*1.2)
		}
	}
}

// iconButton is a bare icon that highlights on hover.
func iconButton(i shape, color widget.Color, onClick func()) widget.Widget {
	return button.New(
		button.SizeOpt(button.Small),
		button.PainterOpt(iconPainter{icon: i, fg: color}),
		button.OnClick(onClick),
	).MinWidth(32)
}

// iconPainter draws a button as an icon, followed by its label if it has one. The
// button widget has no icons of its own; the default painter's embedded metrics keep
// its sizing.
type iconPainter struct {
	button.DefaultPainter
	icon shape
	fg   widget.Color
	bg   *widget.Color // nil: no background, except a highlight on hover
}

func (p iconPainter) PaintButton(canvas widget.Canvas, st button.PaintState) {
	b := st.Bounds
	if b.IsEmpty() {
		return
	}
	bg := background // what's behind the button
	switch {
	case p.bg != nil:
		bg = *p.bg
		if st.Pressed {
			bg = bg.Lerp(widget.ColorBlack, 0.15)
		} else if st.Hovered {
			bg = bg.Lerp(trackColor, 0.15)
		}
		canvas.DrawRoundRect(b, bg, 6)
	case st.Hovered || st.Pressed:
		bg = trackColor
		canvas.DrawRoundRect(b, bg, 6)
	}

	const size, pad, gap = 16, 12, 6
	y := b.Min.Y + (b.Height()-size)/2
	if st.Text == "" {
		p.icon.draw(canvas, geometry.NewRect(b.Min.X+(b.Width()-size)/2, y, size, size), p.fg, bg)
		return
	}
	p.icon.draw(canvas, geometry.NewRect(b.Min.X+pad, y, size, size), p.fg, bg)
	textX := b.Min.X + pad + size + gap
	canvas.DrawText(st.Text, geometry.NewRect(textX, b.Min.Y, b.Max.X-pad-textX, b.Height()), 13, p.fg, true, widget.TextAlignLeft)
}
