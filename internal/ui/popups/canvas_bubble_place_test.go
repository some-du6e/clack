package popups

import (
	"image"
	"testing"
)

// The canvas card's bubble (Quip's BubbleImpl with arrows up and down):
// under its mention or over it only where it fits all in the window,
// else shifted into the window (shiftIntoViewportIfNoDirectionFits).
// A card following its mention meets anchors outside the window.
func TestCanvasBubblePlace(t *testing.T) {
	win, sz := image.Pt(1000, 800), image.Pt(322, 80)
	for _, c := range []struct {
		name   string
		anchor image.Rectangle
		want   image.Point
	}{
		{"under", image.Rect(100, 200, 160, 222), image.Pt(100, 222)},
		{"over, at the window's foot", image.Rect(100, 760, 160, 782), image.Pt(100, 680)},
		{"above the window's top", image.Rect(100, -60, 160, -38), image.Pt(100, 0)},
		{"below the window's foot", image.Rect(100, 900, 160, 922), image.Pt(100, 720)},
		{"right edge", image.Rect(900, 200, 960, 222), image.Pt(678, 222)},
	} {
		if got := canvasBubblePlace(c.anchor, sz, win); got != c.want {
			t.Errorf("%s: %v for a mention at %v, want %v", c.name, got, c.anchor, c.want)
		}
	}
}
