package popups

// A press-only scrim (a backdrop that takes the press dismissing it; its
// release goes to no handler) with something laid out before it that
// executes a command in the middle of layout: a focused editor whose text
// changed (SelectionCmd, SnippetCmd: Gio patch 64) or a ticker
// (op.InvalidateCmd: patch 61, the 11-17 lost click). The router replayed
// the queued press without the release, the pointer stayed down on the
// gone scrim, and the next press, on the button under where it was, went
// nowhere.

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestScrimKeepsLaterPresses(t *testing.T) {
	th := theme.New(false)
	cases := []struct {
		name string
		// before is laid out first, in frame n
		before func(gtx layout.Context, ed *widget.Editor, n int)
	}{
		{"editor", func(gtx layout.Context, ed *widget.Editor, n int) {
			if n == 0 {
				gtx.Execute(key.FocusCmd{Tag: ed})
			}
			if n == 1 {
				ed.SetText("synthetic draft") // its selection and snippet change in this frame's layout
			}
			g := gtx
			g.Constraints = layout.Exact(image.Pt(160, 24))
			th.Editor(g, ed, 14, color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}, color.NRGBA{R: 0xb3, G: 0xd7, B: 0xff, A: 0xff})
		}},
		{"ticker", func(gtx layout.Context, ed *widget.Editor, n int) {
			gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(500 * time.Millisecond)})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := new(input.Router)
			ed := new(widget.Editor)
			var scrim int
			var btn widget.Clickable
			shown, clicks := true, 0
			now := time.Unix(1_800_000_000, 0)
			n := 0
			frame := func() {
				var ops op.Ops
				gtx := layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
					Constraints: layout.Exact(image.Pt(600, 400))}
				c.before(gtx, ed, n)
				for btn.Clicked(gtx) {
					clicks++
				}
				st := op.Offset(image.Pt(300, 100)).Push(&ops)
				g := gtx
				g.Constraints = layout.Exact(image.Pt(100, 40))
				btn.Layout(g, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} })
				st.Pop()
				if shown {
					for {
						ev, ok := gtx.Event(pointer.Filter{Target: &scrim, Kinds: pointer.Press})
						if !ok {
							break
						}
						if pe, ok := ev.(pointer.Event); ok && pe.Kind == pointer.Press {
							shown = false
						}
					}
				}
				if shown {
					st := clip.Rect(image.Rect(0, 0, 600, 400)).Push(&ops)
					event.Op(&ops, &scrim)
					st.Pop()
				}
				r.Frame(&ops)
				n++
				now = now.Add(16 * time.Millisecond)
			}
			click := func(at time.Duration) {
				p := f32.Pt(350, 120)
				r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: at},
					pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: at + 80*time.Millisecond})
			}
			frame() // the scrim up, the editor focused
			click(time.Second)
			frame() // the scrim takes the press and goes
			if shown {
				t.Fatal("the scrim never read its press")
			}
			frame()
			click(3 * time.Second)
			frame()
			if clicks != 1 {
				t.Errorf("%s: %d clicks on the button once the scrim was gone, want 1", c.name, clicks)
			}
		})
	}
}
