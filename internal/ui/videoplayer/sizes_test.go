package videoplayer

// Round 3's review: MAJOR 6, the overlay's size classes on narrow tiles
// (fIJO in gallery contexts: buttonSize x-small at 154 CSS px or less,
// no mute or volume at 138 or less), and the controls container's empty
// parts, which take no click (a MINOR). On the round-3 head (0cbe7e63)
// every width drew the 32px mute, past a 116px tile's edge, and a click
// on the container's empty part toggled the video.

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// layoutAt lays the tile out at size (1 px a dp).
func layoutAt(c *Controller, r *Row, f *richtext.File, size image.Point) {
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(size), Source: router.Source()}
	c.BeginFrame()
	r.LayoutMedia(gtx, richview.MediaSlot{File: f, Poster: image.Rectangle{Max: size}})
	c.EndFrame()
	router.Frame(&ops)
}

func TestOverlaySizeClasses(t *testing.T) {
	c, _ := slowEnv(t, 0, media.FakeOptions{})
	f := videoFile("F1")
	for _, tc := range []struct {
		w     int
		mute  int // the mute button's width, 0 for none
		label string
	}{
		{300, 32, "small"},
		{150, 28, "x-small"},
		{116, 0, "x-small, no volume (a portrait phone video in the thread pane)"},
	} {
		r := c.Row("channel", "C1", "1.0", nil)
		layoutAt(c, r, f, image.Pt(tc.w, tc.w*16/9))
		st := state(t, c, r, f)
		if got := st.Mute.Dx(); got != tc.mute {
			t.Errorf("%dpx wide (%s): the mute button %dpx wide (%v), want %d", tc.w, tc.label, got, st.Mute, tc.mute)
		}
		if !st.Mute.Empty() && st.Mute.Max.X > tc.w-8 {
			t.Errorf("%dpx wide: the mute button %v runs past the controls (the tile is %dpx)", tc.w, st.Mute, tc.w)
		}
	}
}

// A press and release on the controls bar's empty part right of the mute
// button does nothing; the same on the tile above the bar plays it.
func TestControlsBarEmptyPartTakesNoClick(t *testing.T) {
	c, _ := slowEnv(t, 0, media.FakeOptions{})
	r := c.Row("channel", "C1", "1.0", nil)
	f := videoFile("F1")
	layoutAt(c, r, f, image.Pt(480, 270))
	c.mu.Lock()
	tl := c.tiles[r.key(f)]
	z := tl.z
	c.mu.Unlock()
	if z.mute.Empty() {
		t.Fatal("no mute button laid out")
	}
	empty := image.Pt(z.mute.Max.X+40, (z.mute.Min.Y+z.mute.Max.Y)/2)
	if got := z.at(empty, false); got != zNone {
		t.Errorf("the bar's empty part at %v is zone %d, want none (no click of its own)", empty, got)
	}
	if got := z.at(image.Pt(240, 60), false); got != zTile {
		t.Errorf("the tile above the bar is zone %d, want the overlay's button", got)
	}
}
