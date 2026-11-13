package popups

// Slack's popup borders are `1px solid …` in CSS px, which Chromium
// snaps to whole device pixels (StyleBuilderConverter::ClampLineWidth:
// under one device pixel draws one, else floor). polish-1113 found ~35
// popups drawing them as one device pixel (r.Inset(1)), thinner than
// Slack's at 2x. The rule, a scan that fails on a device-pixel border
// coming back, and the drawn width of two checkboxes at each scale.
// With BORDER_SHOTS_DIR set (an area workflow) the bordered
// controls are saved at 1.25x and 2x, before (one device pixel) and after.

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestBorderWidthSnapsAsChromium(t *testing.T) {
	for _, c := range []struct {
		scale, css float32
		want       int
	}{
		{1, 1, 1}, {1.25, 1, 1}, {1.5, 1, 1}, {1.75, 1, 1}, {2, 1, 2}, {2.25, 1, 2}, {2.5, 1, 2}, {3, 1, 3},
		{1.9999999, 1, 2}, {0.5, 1, 1}, {1, 0.5, 1}, {1, 0, 0}, {1.5, 2, 3}, {1.25, 2, 2},
	} {
		gtx := layout.Context{Metric: unit.Metric{PxPerDp: c.scale, PxPerSp: c.scale}}
		if got := borderW(gtx, c.css); got != c.want {
			t.Errorf("a %vpx border at %vx: %d device px, want %d", c.css, c.scale, got, c.want)
		}
	}
}

// No popup draws a CSS border as one device pixel: r.Inset(1) inside a
// 1px border, or a border width of maxInt(1, dp(gtx, 1)) (Gio's Dp
// rounds: 2 at 1.5x where Chromium draws 1). Focus rings (box-shadow
// spreads) and text underlines aren't borders and keep theirs.
func TestPopupBordersAreCSSPixels(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	notBorder := []string{"one :=", "ring, one", "cl.Link)", ", col)", "cl.Focus)"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
	line:
		for i, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "//") {
				continue
			}
			if strings.Contains(l, ".Inset(1)") {
				t.Errorf("%s:%d draws a one-device-pixel border (use border1): %s", f, i+1, strings.TrimSpace(l))
				continue
			}
			if strings.Contains(l, "(1, dp(gtx, 1))") {
				for _, s := range notBorder {
					if strings.Contains(l, s) {
						continue line
					}
				}
				t.Errorf("%s:%d sizes a border with Gio's rounding (use border1): %s", f, i+1, strings.TrimSpace(l))
			}
		}
	}
}

// borderRun is how many pixels from x0 along row y differ from bg.
func borderRun(img *image.RGBA, x0, y int, bg color.NRGBA) int {
	n := 0
	for x := x0; x < img.Bounds().Max.X; x++ {
		c := img.RGBAAt(x, y)
		if near(c, bg) {
			break
		}
		n++
	}
	return n
}

func near(c color.RGBA, n color.NRGBA) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(c.R, n.R) <= 8 && d(c.G, n.G) <= 8 && d(c.B, n.B) <= 8
}

// The search filter's and the message actions' checkboxes, drawn at each
// scale: the ring is borderW device pixels on every side.
func TestPopupBorderDrawnWidth(t *testing.T) {
	cl := colorsOf(theme.New(false))
	for _, scale := range []float32{1, 1.25, 1.5, 1.75, 2, 3} {
		var ops op.Ops
		const w, h = 200, 120
		paint.FillShape(&ops, color.NRGBA{R: 0x40, G: 0x80, B: 0x40, A: 0xff}, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Now: time.Now(),
			Constraints: layout.Exact(image.Pt(w, h))}
		p1, p2 := image.Pt(10, 10), image.Pt(100, 10)
		srFacetCheckbox(gtx, p1, false)
		actCheckbox(gtx, cl, p2, false)
		img := pixeltest.Render(t, w, h, &ops)
		want := borderW(gtx, 1)
		for _, c := range []struct {
			name string
			p    image.Point
			sz   int
			fill color.NRGBA
		}{{"search filter checkbox", p1, dp(gtx, 13), rgb(0xffffff)}, {"message actions checkbox", p2, dp(gtx, 16), cl.Bg}} {
			mid := c.p.Y + c.sz/2
			if got := borderRun(img, c.p.X, mid, c.fill); got != want {
				t.Errorf("%s at %vx: the left border is %d device px, want %d", c.name, scale, got, want)
			}
			// top edge: down the middle column
			n := 0
			for y := c.p.Y; y < c.p.Y+c.sz && !near(img.RGBAAt(c.p.X+c.sz/2, y), c.fill); y++ {
				n++
			}
			if n != want {
				t.Errorf("%s at %vx: the top border is %d device px, want %d", c.name, scale, n, want)
			}
		}
	}
}

// TestPopupBorderShots saves bordered popup controls at 1.25x and 2x,
// light and dark, before (one device pixel, cssBorders off) and after,
// for an area workflow (BORDER_SHOTS_DIR). Synthetic content only.
func TestPopupBorderShots(t *testing.T) {
	dir := os.Getenv("BORDER_SHOTS_DIR")
	if dir == "" {
		t.Skip("BORDER_SHOTS_DIR not set")
	}
	sh := theme.NewShaper()
	var ed widget.Editor
	var btn widget.Clickable
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th := theme.New(dark)
		th.SetShapers(sh, sh)
		cl := colorsOf(th)
		for _, scale := range []float32{1.25, 2} {
			for _, after := range []bool{false, true} {
				cssBorders = after
				w, h := int(320*scale), int(170*scale)
				var ops op.Ops
				paint.FillShape(&ops, cl.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
				gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Now: time.Now(),
					Constraints: layout.Exact(image.Pt(w, h))}
				srFacetCheckbox(gtx, image.Pt(dp(gtx, 16), dp(gtx, 16)), false)
				actCheckbox(gtx, cl, image.Pt(dp(gtx, 40), dp(gtx, 14)), false)
				at(gtx, image.Pt(dp(gtx, 66), dp(gtx, 14)), func(gtx layout.Context) layout.Dimensions {
					return Checkbox(gtx, th, false, false, false) // .c-input_checkbox
				})
				at(gtx, image.Pt(dp(gtx, 16), dp(gtx, 44)), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Exact(image.Pt(dp(gtx, 288), dp(gtx, 36)))
					return Field(gtx, th, &ed, "Search", "search", 36, 15)
				})
				at(gtx, image.Pt(dp(gtx, 16), dp(gtx, 96)), func(gtx layout.Context) layout.Dimensions {
					return Button(gtx, th, &btn, BtnOutline, BtnMedium, "Cancel", "", 80)
				})
				img := pixeltest.Render(t, w, h, &ops)
				name := "borders-" + mode + "-" + map[float32]string{1.25: "125", 2: "200"}[scale] + "-" +
					map[bool]string{false: "before", true: "after"}[after] + ".png"
				f, err := os.Create(filepath.Join(dir, name))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, img); err != nil {
					t.Fatal(err)
				}
				f.Close()
			}
		}
	}
	cssBorders = true
}
