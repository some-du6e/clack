package popups

import (
	"image"
	"testing"
)

// keepInBounds around a 28px button: "right-bottom" with offsetX 4 opens
// beside it; at the window's right edge it shifts left to 16px inside
// (no flip); near the bottom, tryOppositePosition takes "right-top".
func TestSlackPlace(t *testing.T) {
	win := image.Pt(1440, 900)
	sz := image.Pt(240, 300)
	btn := image.Rect(600, 200, 628, 228)
	if p := slackPlace(btn, sz, win, "right-bottom", 4, 0, 16, 10, true); p != image.Pt(632, 200) {
		t.Errorf("beside: %v", p)
	}
	edge := image.Rect(1390, 200, 1418, 228)
	if p := slackPlace(edge, sz, win, "right-bottom", 4, 0, 16, 10, true); p != image.Pt(1440-16-240, 200) {
		t.Errorf("right edge: %v", p)
	}
	low := image.Rect(600, 700, 628, 728)
	if p := slackPlace(low, sz, win, "right-bottom", 4, 0, 16, 10, true); p != image.Pt(632, 728-300) {
		t.Errorf("near the bottom: %v", p)
	}
	if p := slackPlace(low, sz, win, "right-bottom", 4, 0, 16, 10, false); p != image.Pt(632, 900-10-300) {
		t.Errorf("near the bottom, no opposite: %v", p)
	}
	if p := slackPlace(btn, image.Pt(232, 42), win, "top-right", 0, 0, 16, 10, false); p != image.Pt(628-232, 200-42) {
		t.Errorf("top-right: %v", p)
	}
}
