package popups

// A press on a widget laid out after something that ticks reaches it
// whole: a tooltip in its hover delay, a loading spinner, a peek card
// animating in, a focused editor's caret. Each executes an
// op.InvalidateCmd in the middle of layout, which (Clack patch 61) asks
// for its frame and no longer replays the queued input; the wakeup still
// comes when the ticker needs it.

import (
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestTickersKeepLaterPresses(t *testing.T) {
	th := theme.New(false)
	t0 := time.Unix(1_800_000_000, 0)
	type ticker struct {
		name string
		tick func(gtx layout.Context, first bool)
		// wake checks the router's wakeup after the frame with the press
		wake func(now time.Time, w time.Time) bool
	}
	var tip TipState
	var spin widget.Clickable
	ed := new(widget.Editor)
	pop := NewPopover(&tzHost{th: th}, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(100, 40)}
	}, PopoverOpts{Peek: true, AnimateIn: true, Anchor: image.Rect(20, 300, 60, 320)})
	tickers := []ticker{
		{"tooltip", func(gtx layout.Context, first bool) {
			Tooltip(gtx, th, &tip, true, image.Pt(40, 20), Below, "Synthetic tip", "")
		}, func(now, w time.Time) bool { return w.Equal(t0.Add(tip.delay())) }},
		{"spinner", func(gtx layout.Context, first bool) {
			LoadingButton(gtx, th, &spin, BtnMedium, 80)
		}, func(now, w time.Time) bool { return w.Equal(now.Add(16 * time.Millisecond)) }},
		{"animate-in", func(gtx layout.Context, first bool) {
			pop.Layout(gtx)
		}, func(now, w time.Time) bool { return w.IsZero() }}, // a frame per vsync until it's in
		{"caret", func(gtx layout.Context, first bool) {
			if first {
				gtx.Execute(key.FocusCmd{Tag: ed})
			}
			g := gtx
			g.Constraints = layout.Exact(image.Pt(160, 24))
			th.Editor(g, ed, 14, color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}, color.NRGBA{R: 0xb3, G: 0xd7, B: 0xff, A: 0xff})
		}, func(now, w time.Time) bool {
			return w.After(now) && !w.After(now.Add(widget.CaretBlinkInterval()))
		}},
	}
	for _, k := range tickers {
		t.Run(k.name, func(t *testing.T) {
			r := new(input.Router)
			var btn widget.Clickable
			clicks := 0
			now := t0
			frame := func(first bool) {
				var ops op.Ops
				gtx := layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
					Constraints: layout.Exact(image.Pt(600, 400))}
				k.tick(gtx, first) // laid out first, ticking
				for btn.Clicked(gtx) {
					clicks++
				}
				st := op.Offset(image.Pt(300, 100)).Push(&ops)
				g := gtx
				g.Constraints = layout.Exact(image.Pt(100, 40))
				btn.Layout(g, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Min} })
				st.Pop()
				r.Frame(&ops)
			}
			frame(true)
			r.WakeupTime() // the first frame's
			p := f32.Pt(350, 120)
			r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: 1},
				pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: 2})
			now = now.Add(16 * time.Millisecond)
			frame(false)
			if clicks != 1 {
				t.Errorf("%d clicks on the button laid out after the %s, want 1", clicks, k.name)
			}
			if h := btn.History(); len(h) != 1 {
				t.Errorf("the button's history has %d presses, want 1", len(h))
			}
			if w, ok := r.WakeupTime(); !ok || !k.wake(now, w) {
				t.Errorf("the %s's wakeup %v (%v) at frame %v: not its cadence", k.name, w, ok, now)
			}
		})
	}
}
