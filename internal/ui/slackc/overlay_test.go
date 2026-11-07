package slackc

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// A plugin element in document.body (oneko's cat: position fixed,
// pointer-events none) must not swallow clicks meant for the native UI
// under it; a body child that takes pointer events blocks only its own box.
func TestOverlayBodyLetsClicksThrough(t *testing.T) {
	withStyles(t, func(n *Node, s *css.Style) {
		switch n.ID {
		case "neko":
			s.Position = css.PosFixed
			s.Left, s.Top = px(100), px(100)
			s.Width, s.Height = px(32), px(32)
			s.PointerEvents = false
		case "card":
			s.Position = css.PosFixed
			s.Left, s.Top = px(300), px(300)
			s.Width, s.Height = px(50), px(50)
		}
	})
	windowSize = image.Pt(400, 400)
	deliverOverlay(OverlayBody, mustTree(t, `[{"t":"div","k":1,"p":{"id":"neko"}},{"t":"div","k":2,"p":{"id":"card"}}]`))
	defer deliverOverlay(OverlayBody, nil)

	var r input.Router
	var btn widget.Clickable
	clicks := 0
	frame := func() {
		gtx := testGtx(400, 400)
		gtx.Source = r.Source()
		for btn.Clicked(gtx) {
			clicks++
		}
		btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Max} })
		LayoutOverlays(gtx)
		r.Frame(gtx.Ops)
	}
	click := func(p f32.Point) int {
		before := clicks
		frame()
		r.Queue(
			pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: 10 * time.Millisecond},
			pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: 20 * time.Millisecond},
			pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: 30 * time.Millisecond},
		)
		frame()
		frame()
		return clicks - before
	}
	if n := click(f32.Pt(20, 20)); n != 1 {
		t.Errorf("click on the native UI beside the body's elements: %d clicks, want 1", n)
	}
	if n := click(f32.Pt(110, 110)); n != 1 {
		t.Errorf("click through the pointer-events:none element: %d clicks, want 1", n)
	}
	if n := click(f32.Pt(320, 320)); n != 0 {
		t.Errorf("click on a body element that takes pointer events reached the native under it (%d clicks)", n)
	}
}
