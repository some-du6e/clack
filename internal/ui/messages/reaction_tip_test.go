package messages

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

func TestReactionTipText(t *testing.T) {
	re := &core.Reaction{Name: "tada", Count: 5, Users: []string{"USELF", "U1", "U2", "U3", "U4"}}
	got := reactionTip(nilHost{}, re, "USELF")
	if !strings.HasPrefix(got, "You, U1, U2 and 2 others") || !strings.HasSuffix(got, " reacted with :tada:") {
		t.Fatalf("%q", got)
	}
	if got := reactionTip(nilHost{}, &core.Reaction{Name: "eyes", Count: 1, Users: []string{"U1"}}, "USELF"); got != "U1 reacted with :eyes:" {
		t.Fatalf("%q", got)
	}
}

// The reaction tooltip: a dark box above the pill with the light emoji
// tile and the (wrapped) text, inside the window.
func TestReactionTooltipDraws(t *testing.T) {
	th := theme.New(false)
	c := colorsFor(th)
	const w, h = 400, 300
	var ops op.Ops
	paint.FillShape(&ops, c.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h)), Now: time.Now()}
	long := "You, Alice Example, Bob Example and 12 others reacted with :large_green_circle:"
	anchor := image.Pt(380, 250) // near the right edge: kept inside
	reactionTooltip(gtx, nilHost{}, th, c, long, "", anchor, image.Pt(w, h))
	img := pixeltest.Render(t, w, h, &ops)
	box := image.Rect(0, 0, w, anchor.Y)
	if n := pixeltest.Count(img, box, c.Tooltip, 4); n < 3000 {
		t.Fatalf("tooltip box %d px", n)
	}
	if n := pixeltest.Count(img, image.Rect(w-4, 0, w, h), c.Tooltip, 4); n > 0 {
		t.Errorf("tooltip runs off the window's right edge (%d px)", n)
	}
	if n := pixeltest.Count(img, box, rgb(0xffffff), 2); n < 44*44-60 {
		t.Errorf("no emoji tile (%d white px)", n)
	}
	if n := pixeltest.Ink(img, box, c.TooltipText, c.Tooltip); n < 60 {
		t.Errorf("no text (%d px)", n)
	}
	// the text wrapped onto several lines instead of running out of the box
	rows := 0
	for y := 0; y < anchor.Y; y++ {
		if pixeltest.Ink(img, image.Rect(0, y, w, y+1), c.TooltipText, c.Tooltip) > 0 {
			rows++
		}
	}
	if rows < 18 {
		t.Errorf("text spans %d px rows, want 2+ lines", rows)
	}
}
