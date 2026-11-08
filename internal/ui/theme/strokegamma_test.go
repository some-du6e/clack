package theme

import (
	"image/color"
	"math"
	"testing"
)

// TestStrokeGamma: the edge model's widths for the inks the icons use
// (values from a coverage-exact rasterizer blending in linear light vs
// sRGB, see StrokeGamma), and no allocation on the draw path.
func TestStrokeGamma(t *testing.T) {
	for _, c := range []struct {
		ink  uint32
		want float32
	}{
		{ink: 0xb9babd, want: -0.27},  // dark composer icons
		{ink: 0xd1d2d3, want: -0.30},  // dark primary
		{ink: 0xffffff, want: -0.33},  // white on the dark rail
		{ink: 0x454447, want: +0.24},  // light composer icons
		{ink: 0x1d1c1d, want: +0.32},  // light primary
		{ink: 0x868686, want: +0.135}, // light subtle (below the 0.6 split: white bg)
	} {
		col := color.NRGBA{R: uint8(c.ink >> 16), G: uint8(c.ink >> 8), B: uint8(c.ink), A: 0xff}
		if got := StrokeGamma(col); math.Abs(float64(got-c.want)) > 0.03 {
			t.Errorf("StrokeGamma(#%06x) = %.3f, want %.3f", c.ink, got, c.want)
		}
	}
	if g := StrokeGamma(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x80}); g >= 0 || g < -0.4 {
		t.Errorf("half-alpha white = %.3f, want a smaller negative", g)
	}
	if g := StrokeGamma(color.NRGBA{}); g != 0 {
		t.Errorf("transparent = %v", g)
	}
	col := color.NRGBA{R: 0xb9, G: 0xba, B: 0xbd, A: 0xff}
	if n := testing.AllocsPerRun(100, func() { _ = StrokeGamma(col) }); n != 0 {
		t.Errorf("StrokeGamma allocates: %v", n)
	}
}
