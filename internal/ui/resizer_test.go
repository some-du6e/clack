package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// rzPress presses the primary button at p (window px) and lays out a frame.
func (e *perfEnv) rzPress(p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
	)
	e.frame()
}

// rzMove moves the pointer to p with the button held, in one event (a
// jerk: the pointer leaves the handle at once), and lays out one frame.
func (e *perfEnv) rzMove(p image.Point) {
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.frame()
}

// rzRelease lets go at p (outside the window is fine) and lays out a frame.
func (e *perfEnv) rzRelease(p image.Point) {
	e.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.frame()
}

// Karim, 2026-11-12 (Windows): dragging the sidebar "is not instant", and
// "if I just do a quick jerk to the side … I am no longer selecting it,
// even though I have held down my mouse button". BaseResizer follows
// mousemove on the document until mouseup: every resizer here grabs the
// pointer on press, keeps following it however far and fast it goes
// (outside the window too), draws the new width in the frame the move
// arrives in, and stops at a release outside the window.
func TestResizersHoldThePointer(t *testing.T) {
	e, _ := previewEnv(t)
	e.frame()
	a := e.a
	rail := int(theme.RailW)

	t.Run("sidebar", func(t *testing.T) {
		s := &a.sidebar
		w0 := s.width
		y := a.primaryOrigin.Y + 200
		edge := rail + w0
		drawn := func(want int, what string) {
			t.Helper()
			if s.width != want || a.primaryOrigin.X != rail+want {
				t.Errorf("%s: width %d, drawn edge at %d; want %d drawn at %d in the same frame", what, s.width, a.primaryOrigin.X, want, rail+want)
			}
		}
		e.rzPress(image.Pt(edge, y))
		if !s.rz.dragging() {
			t.Fatal("the press on the handle didn't start a drag")
		}
		e.rzMove(image.Pt(edge+5, y))
		drawn(w0+5, "5px right")
		e.rzMove(image.Pt(edge-100, y+300)) // into the messages, down the window
		drawn(w0-100, "a jerk 100px left")
		e.rzMove(image.Pt(edge+60, y-150))
		drawn(w0+60, "back 160px right")
		e.rzMove(image.Pt(-400, y)) // out of the window
		drawn(sbWidthMin, "out of the window on the left")
		e.rzMove(image.Pt(e.size.X+400, y))
		drawn(sbWidthMax, "out of the window on the right")
		e.rzRelease(image.Pt(e.size.X+400, y+50))
		if s.rz.dragging() || s.mode != "manual" || s.intent != sbWidthMax {
			t.Errorf("released outside the window: dragging %v, %d %q; want a manual %d", s.rz.dragging(), s.intent, s.mode, sbWidthMax)
		}
		e.movePtr(image.Pt(rail+250, y)) // no button: nothing follows
		drawn(sbWidthMax, "a move after the release")
		s.intent, s.mode = 0, "auto"
		s.rz.lastPress = time.Time{}
		e.frame()
	})

	t.Run("secondary", func(t *testing.T) {
		ch, _ := splitChannels(t, e)
		e.openChannel(ch)
		a.OpenDetails(ch)
		e.settle(200 * time.Millisecond)
		s := &a.secondary
		if !s.open() || s.drawnW == 0 {
			t.Fatal("no details pane")
		}
		panel := e.size.X - int(theme.PanelInset) - a.primaryOrigin.X
		edge := func() int { return a.secondaryOrigin.X - 1 } // the pane's left edge, window px
		w0 := s.drawnW
		y := a.primaryOrigin.Y + 300
		drawn := func(want int, what string) {
			t.Helper()
			if s.drawnW != want || edge() != a.primaryOrigin.X+panel-want {
				t.Errorf("%s: pane %dpx, edge at %d; want %d drawn at %d in the same frame", what, s.drawnW, edge(), want, a.primaryOrigin.X+panel-want)
			}
		}
		x := edge()
		e.rzPress(image.Pt(x, y))
		e.rzMove(image.Pt(x-40, y))
		drawn(w0+40, "40px left")
		e.rzMove(image.Pt(x+90, y-200)) // a jerk into the pane
		drawn(w0-90, "a jerk 90px right")
		e.rzMove(image.Pt(-300, y)) // past the sidebar, out of the window
		drawn(panel-300, "out of the window on the left (the primary keeps 300)")
		e.rzMove(image.Pt(x+30, e.size.Y+300)) // below the window
		drawn(w0-30, "below the window")
		e.rzRelease(image.Pt(x+30, e.size.Y+300))
		if s.rz.dragging() {
			t.Error("still dragging after a release below the window")
		}
		e.movePtr(image.Pt(x-200, y))
		drawn(w0-30, "a move after the release")
		a.CloseSecondary()
		e.frame()
	})

	t.Run("split", func(t *testing.T) {
		chA, chB := splitChannels(t, e)
		e.openChannel(chA)
		a.OpenSplit("c/" + chB)
		e.settle(300 * time.Millisecond)
		s := &a.split
		right := e.size.X - int(theme.PanelInset)
		w0 := s.col.Dx()
		y := s.card.Min.Y + s.card.Dy()/2
		drawn := func(want int, what string) {
			t.Helper()
			if s.col.Dx() != want || s.col.Min.X != right-want {
				t.Errorf("%s: column %dpx at %d; want %d at %d in the same frame", what, s.col.Dx(), s.col.Min.X, want, right-want)
			}
		}
		x := s.card.Min.X
		e.rzPress(image.Pt(x, y))
		e.rzMove(image.Pt(x-60, y))
		drawn(w0+60, "60px left")
		e.rzMove(image.Pt(x+250, y+200)) // a jerk deep into the card
		drawn(splitMinW, "a jerk 250px right (Slack's 300 minimum)")
		e.rzMove(image.Pt(x-120, -200)) // above the window
		drawn(w0+120, "above the window")
		e.rzRelease(image.Pt(e.size.X+300, -200))
		if s.rz.dragging() || s.mode != "manual" {
			t.Errorf("released outside the window: dragging %v, mode %q", s.rz.dragging(), s.mode)
		}
		e.movePtr(image.Pt(x+100, y))
		drawn(w0+120, "a move after the release")
	})
}
