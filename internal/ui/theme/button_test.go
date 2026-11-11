package theme

import (
	"image/color"
	"testing"
)

// The v2 Button's metrics per size (Panda tokens: sizes 8/9/11, radii
// base/large/xlarge, font sizes caption/base/subtitle).
func TestButtonV2Metrics(t *testing.T) {
	for _, c := range []struct {
		s                 ButtonV2Size
		h, rad, font, ico float32
	}{
		{ButtonV2Small, 32, 4, 13, 16},
		{ButtonV2Medium, 36, 8, 15, 18},
		{ButtonV2Large, 44, 12, 18, 20},
	} {
		h, rad, font, ico := ButtonV2Metrics(c.s)
		if h != c.h || rad != c.rad || font != c.font || ico != c.ico {
			t.Errorf("size %d: %v %v %v %v", c.s, h, rad, font, ico)
		}
	}
}

// The variations' colours are the tokens of each mode; hover is the
// -hover token blended in (multiply light, lighten dark).
func TestButtonV2Palette(t *testing.T) {
	lp, dp := ButtonV2Palette(New(false), "primary"), ButtonV2Palette(New(true), "primary")
	if lp.Bg != rgb(0x007a5a) || lp.Fg != rgb(0xffffff) || lp.Border.A != 0 {
		t.Errorf("light primary %+v", lp)
	}
	if dp.Bg != rgb(0x177e56) || dp.Fg != rgb(0xf8f8f8) {
		t.Errorf("dark primary %+v", dp)
	}
	// #007a5a multiplied by #0a4032 at 0x21: about #006e51
	if d := diff(lp.Hover, rgb(0x006e51)); d > 2 {
		t.Errorf("light primary hover %v", lp.Hover)
	}
	// lighten with #d8f5e9 at 0x14 lifts every channel
	if dp.Hover.R <= dp.Bg.R || dp.Hover.G <= dp.Bg.G || dp.Hover.B <= dp.Bg.B {
		t.Errorf("dark primary hover %v over %v", dp.Hover, dp.Bg)
	}
	sec := ButtonV2Palette(New(false), "secondary")
	if sec.Bg != rgb(0xffffff) || sec.Fg != rgb(0x1d1c1d) || sec.Border != rgba(0x5e5d60, 0x21) {
		t.Errorf("secondary %+v", sec)
	}
	dis := ButtonV2Palette(New(true), "disabled")
	if dis.Bg != rgb(0x212428) || dis.Fg != rgb(0xb9babd) || dis.Hover != dis.Bg {
		t.Errorf("dark disabled %+v", dis)
	}
	if f := ButtonV2Focus(New(false)); f != rgb(0x1264a3) {
		t.Errorf("focus ring %v", f)
	}
}

// A stylesheet's base-inv-hgl-2 recolours the primary button.
func TestButtonV2FollowsTokens(t *testing.T) {
	th := New(false)
	th.Tokens = fakeTokens{"var(--dt_color-base-inv-hgl-2)": rgb(0x5e81ac)}
	if c := ButtonV2Palette(th, "primary"); c.Bg != rgb(0x5e81ac) {
		t.Errorf("themed primary %v", c.Bg)
	}
}

func diff(a, b color.NRGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return max(d(a.R, b.R), d(a.G, b.G), d(a.B, b.B))
}
