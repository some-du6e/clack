package popups

// A timed revert ("Copied!" for 2 s, the toolbar's "Query copied!" for
// 3 s) asks for its frame in every frame it's pending, not only in the
// click's: the window keeps one wakeup, the earliest asked since its last
// frame, so a frame in between (the pointer moved, a tooltip's delay ran
// out) that didn't ask again left the label up until something else drew.

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// wakesDuring runs frame and returns the wakeups its op.InvalidateCmds
// asked for.
func wakesDuring(frame func()) []time.Time {
	var at []time.Time
	input.ExecuteHook = func(c input.Command) {
		if inv, ok := c.(op.InvalidateCmd); ok {
			at = append(at, inv.At)
		}
	}
	defer func() { input.ExecuteHook = nil }()
	frame()
	return at
}

func asked(at []time.Time, w time.Time) bool {
	for _, a := range at {
		if a.Equal(w) {
			return true
		}
	}
	return false
}

// The URL hover card's Copy link: "Copied!" for 2 s, its end asked for by
// a frame in between too.
func TestURLCardCopiedAsksAgain(t *testing.T) {
	h := &tzHost{th: theme.New(false)}
	p := NewURLHoverCard(h, image.Rect(100, 100, 220, 120), "https://example.test/synthetic")
	var r input.Router
	now := time.Unix(1_800_000_000, 0)
	frame := func() {
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Now: now, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(1000, 700))}
		p.Layout(gtx)
		r.Frame(&ops)
	}
	frame()
	p.copy.Click()
	frame()
	if got := p.CopyTip(now); got != "Copied!" {
		t.Fatalf("after the click the tip is %q", got)
	}
	now = now.Add(100 * time.Millisecond)
	if at := wakesDuring(frame); !asked(at, p.copiedUntil) {
		t.Errorf("a frame while Copied! shows asked for %v, not its end at %v", at, p.copiedUntil)
	}
}
