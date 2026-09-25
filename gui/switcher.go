package gui

import (
	"sync/atomic"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/widget"
)

// switcher is a root widget that shows either the main view or a temporary one
// (e.g. a question) and can be flipped from any goroutine. gogpu/ui has no way to
// post work onto its UI thread, so the choice is an atomic pointer that every
// Layout/Draw/Event call reads.
type switcher struct {
	main    widget.Widget
	overlay atomic.Pointer[widget.Widget]
}

func newSwitcher(main widget.Widget) *switcher {
	return &switcher{main: main}
}

// show replaces the main view with w; show(nil) restores the main view.
func (s *switcher) show(w widget.Widget) {
	if w == nil {
		s.overlay.Store(nil)
		return
	}
	s.overlay.Store(&w)
}

func (s *switcher) current() widget.Widget {
	if w := s.overlay.Load(); w != nil {
		return *w
	}
	return s.main
}

func (s *switcher) Layout(ctx widget.Context, c geometry.Constraints) geometry.Size {
	return s.current().Layout(ctx, c)
}

func (s *switcher) Draw(ctx widget.Context, canvas widget.Canvas) {
	s.current().Draw(ctx, canvas)
}

func (s *switcher) Event(ctx widget.Context, e event.Event) bool {
	return s.current().Event(ctx, e)
}

func (s *switcher) Children() []widget.Widget {
	return []widget.Widget{s.current()}
}

// Containers position children through these (unchecked type assertions in
// primitives), so forward them to whichever view is showing.

func (s *switcher) SetBounds(r geometry.Rect) {
	if b, ok := s.current().(interface{ SetBounds(geometry.Rect) }); ok {
		b.SetBounds(r)
	}
}

func (s *switcher) Bounds() geometry.Rect {
	if b, ok := s.current().(interface{ Bounds() geometry.Rect }); ok {
		return b.Bounds()
	}
	return geometry.Rect{}
}
