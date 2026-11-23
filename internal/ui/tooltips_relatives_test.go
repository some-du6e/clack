package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// tipStubOverlay is an overlay that draws nothing (a menu or modal for
// the TooltipManager); peek makes it a hover peek.
type tipStubOverlay struct{ peek bool }

func (tipStubOverlay) Layout(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }
func (tipStubOverlay) Dismissed() bool                             { return false }
func (o tipStubOverlay) Peeking() bool                             { return o.peek }

// Slack's TooltipManager in the shell: BaseTooltip hides on Escape
// (hoverEscHide), whoever takes the key, and opening a modal or a menu
// hides the tooltip up (hideVisibleTooltip); a hover peek doesn't.
func TestShellHidesTooltips(t *testing.T) {
	a := &App{}
	g := tipGenT()
	a.inspPresses = []key.Event{{Name: key.NameEscape, State: key.Press}}
	a.konamiKeys(nil)
	if tipGenT() == g {
		t.Error("an Escape press didn't hide the tooltip")
	}
	g = tipGenT()
	a.ShowOverlay(tipStubOverlay{peek: true})
	if tipGenT() != g {
		t.Error("a hover peek opening hid the tooltip")
	}
	a.ShowOverlay(tipStubOverlay{})
	if tipGenT() == g {
		t.Error("a menu or modal opening didn't hide the tooltip")
	}
}

// A sidebar name that doesn't fit has BaseChannelName's tooltip (delay
// 600, top): the whole name. One that fits has none.
func TestSidebarTruncatedNameTooltip(t *testing.T) {
	a := &App{th: theme.New(false)}
	var r input.Router
	var ops op.Ops
	now := time.Unix(1000, 0)
	rect := image.Rect(40, 0, 140, 28)
	frame := func(trunc bool) {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(300, 100))}
		sidebarNameTipT(a, gtx, "C0LONGNAME", "a-channel-whose-name-is-long", rect, trunc)
		r.Frame(&ops)
		now = now.Add(20 * time.Millisecond)
	}
	frame(true)
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, PointerID: 1, Position: f32.Pt(90, 14)})
	t0 := now
	for now.Sub(t0) < 560*time.Millisecond {
		frame(true)
	}
	if sidebarNameTipShowingT(a, "C0LONGNAME", now) {
		t.Error("the truncated name's tooltip showed before its 600 ms")
	}
	for now.Sub(t0) < 660*time.Millisecond {
		frame(true)
	}
	if !sidebarNameTipShowingT(a, "C0LONGNAME", now) {
		t.Error("no tooltip over the truncated name after 600 ms")
	}
	frame(false)
	frame(false)
	if sidebarNameTipShowingT(a, "C0LONGNAME", now) {
		t.Error("the name fits now, and its tooltip stayed")
	}
}
