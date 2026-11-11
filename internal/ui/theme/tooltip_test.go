package theme

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The tooltip colours are Slack's .c-tooltip__tip tokens in each mode:
// base-inv-pry / content-inv-pry (#1d1c1d / #fff light, #212428 / #f8f8f8
// dark), TooltipSubtitle's content-inv-sec, and the dark-only outline
// (drop-shadow 0 0 1px otl-pry).
func TestTooltipColorsPerMode(t *testing.T) {
	light, dark := New(false).Tooltip(), New(true).Tooltip()
	want := map[string][2]color.NRGBA{
		"bg":       {rgb(0x1d1c1d), rgb(0x212428)},
		"text":     {rgb(0xffffff), rgb(0xf8f8f8)},
		"subtitle": {rgb(0xcbcccf), rgb(0xb9babd)},
		"glow":     {{}, rgb(0x797c81)},
	}
	got := map[string][2]color.NRGBA{
		"bg":       {light.Bg, dark.Bg},
		"text":     {light.Text, dark.Text},
		"subtitle": {light.Subtitle, dark.Subtitle},
		"glow":     {light.Glow, dark.Glow},
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: light %v dark %v, want %v %v", k, got[k][0], got[k][1], w[0], w[1])
		}
	}
}

// A theme's stylesheet reaches the tooltip through its tokens.
func TestTooltipColorsFollowTokens(t *testing.T) {
	th := New(true)
	tk := fakeTokens{"var(--dt_color-base-inv-pry)": rgb(0x102030), "var(--dt_color-otl-pry)": rgb(0x405060)}
	p := DarkPalette
	ApplyTokens(&p, tk, true)
	th.C = p
	if c := th.Tooltip(); c.Bg != rgb(0x102030) || c.Glow != rgb(0x405060) || c.Text != rgb(0xf8f8f8) {
		t.Errorf("themed tooltip %+v", c)
	}
	lp := LightPalette
	ApplyTokens(&lp, tk, false)
	if lp.TooltipGlow != (color.NRGBA{}) {
		t.Errorf("light mode has no outline token, got %v", lp.TooltipGlow)
	}
}

// drop-shadow(0 0 1px): the blur's mean alpha over the first CSS pixel
// outside the edge is about 19% at 1x; 32% then 7% at 2x.
func TestTooltipGlowFalloff(t *testing.T) {
	for _, c := range []struct {
		u0, u1, want float64
	}{{0, 1, 0.19}, {0, 0.5, 0.32}, {0.5, 1, 0.075}} {
		if got := glowBand(c.u0, c.u1); math.Abs(got-c.want) > 0.02 {
			t.Errorf("band [%v,%v): %.3f, want ≈%.3f", c.u0, c.u1, got, c.want)
		}
	}
}

// In dark mode the box and the arrow get a faint outline just outside
// them, in the outline colour; in light mode nothing is drawn outside.
func TestTooltipShapeOutline(t *testing.T) {
	const w, h = 120, 80
	render := func(dark bool) *image.RGBA {
		th := New(dark)
		c := th.Tooltip()
		var ops op.Ops
		paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
		TooltipShape(gtx, image.Rect(20, 20, 100, 50), 8, TooltipArrow{C: image.Pt(60, 50), Half: 6}, c.Bg, c.Glow)
		return pixeltest.Render(t, w, h, &ops)
	}
	dark := render(true)
	th := New(true)
	if n := pixeltest.Count(dark, image.Rect(20, 20, 100, 50), th.C.Tooltip, 2); n < 2000 {
		t.Fatalf("dark box %d px", n)
	}
	// one pixel above the box's top edge: neither the page nor the box
	ring := 0
	for x := 30; x < 90; x++ {
		c := dark.RGBAAt(x, 19)
		if c != (color.RGBA{th.C.Bg.R, th.C.Bg.G, th.C.Bg.B, 0xff}) {
			ring++
		}
	}
	if ring < 50 {
		t.Errorf("no outline above the dark box (%d px)", ring)
	}
	// the arrow's tip (at 60,56) is outlined too
	if c := dark.RGBAAt(60, 56); c == (color.RGBA{th.C.Bg.R, th.C.Bg.G, th.C.Bg.B, 0xff}) {
		t.Errorf("no outline under the arrow's tip: %v", c)
	}
	light := render(false)
	lb := New(false).C.Bg
	for x := 30; x < 90; x++ {
		if c := light.RGBAAt(x, 19); c != (color.RGBA{lb.R, lb.G, lb.B, 0xff}) {
			t.Fatalf("light mode drew outside the box at (%d,19): %v", x, c)
		}
	}
}

type fakeTokens map[string]color.NRGBA

func (f fakeTokens) Color(expr string) (color.NRGBA, bool, bool) {
	c, ok := f[expr]
	return c, ok, ok
}
func (f fakeTokens) Overridden() bool { return len(f) > 0 }
func (f fakeTokens) Key() uint64      { return uint64(len(f)) }
