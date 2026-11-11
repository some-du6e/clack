package slackc

import "testing"

// background-position as Oneko sets it (a sprite frame's pixel offset) and
// the keyword and percentage forms.
func TestBgPosition(t *testing.T) {
	for _, c := range []struct {
		pos              string
		w, h, iw, ih, dp float32
		x, y             float32
	}{
		{"-96px -32px", 32, 32, 256, 128, 1, -96, -32},
		{"-96px -32px", 64, 64, 512, 256, 2, -192, -64}, // CSS px at 2x
		{"0px 0px", 32, 32, 256, 128, 1, 0, 0},
		{"", 100, 50, 20, 10, 1, 0, 0},
		{"center", 100, 50, 20, 10, 1, 40, 20},
		{"center center", 100, 50, 20, 10, 1, 40, 20},
		{"right bottom", 100, 50, 20, 10, 1, 80, 40},
		{"top left", 100, 50, 20, 10, 1, 0, 0},
		{"top", 100, 50, 20, 10, 1, 40, 0},
		{"50% 25%", 100, 50, 20, 10, 1, 40, 10},
		{"10px center", 100, 50, 20, 10, 1, 10, 20},
	} {
		x, y := bgPosition(c.pos, c.w, c.h, c.iw, c.ih, c.dp)
		if x != c.x || y != c.y {
			t.Errorf("%q in %vx%v (image %vx%v, dp %v): %v,%v want %v,%v", c.pos, c.w, c.h, c.iw, c.ih, c.dp, x, y, c.x, c.y)
		}
	}
}
