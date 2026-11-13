package popups

// The popover card's 1px ring is a box-shadow spread, which Chromium
// doesn't snap: at 1.25x it is one device pixel of the ring color plus
// one a quarter covered, at 1.5x half covered, at 2x two whole pixels
// (card_ring.go). With BORDER_SHOTS_DIR set (menu-keys) the card is saved
// at 1.25x and 2x, light and dark, before (one device pixel) and after,
// each with an 8x crop of its top-left corner.

import (
	"image"
	"image/color"
	"image/png"
	"math"
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

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// cssRingColor is the ring's CSS color (#1d1c1d21 light, #d1d2d321 dark).
func cssRingColor(dark bool) color.NRGBA {
	if dark {
		return rgba(0xd1d2d3, .13)
	}
	return rgba(0x1d1c1d, .13)
}

// chromiumRingPixel is the pixel Chromium paints covered cov by the ring
// color over bg: the sRGB blend, alpha times coverage.
func chromiumRingPixel(css, bg color.NRGBA, cov float64) color.NRGBA {
	a := float64(css.A) / 255 * cov
	l := func(f, b uint8) uint8 { return uint8(math.Round(float64(f)*a + float64(b)*(1-a))) }
	return color.NRGBA{R: l(css.R, bg.R), G: l(css.G, bg.G), B: l(css.B, bg.B), A: 0xff}
}

func within(c color.RGBA, n color.NRGBA, tol int) bool {
	d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
	return d(c.R, n.R) <= tol && d(c.G, n.G) <= tol && d(c.B, n.B) <= tol
}

func TestCardRingLikeChromium(t *testing.T) {
	for _, dark := range []bool{false, true} {
		cl := colorsOf(theme.New(dark))
		bg, css := cl.Bg, cssRingColor(dark)
		for _, scale := range []float32{1, 1.25, 1.5, 1.75, 2, 2.5, 3} {
			const w, h = 200, 140
			var ops op.Ops
			paint.FillShape(&ops, bg, clip.Rect{Max: image.Pt(w, h)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Now: time.Now(),
				Constraints: layout.Exact(image.Pt(w, h))}
			r := image.Rect(40, 40, 160, 100)
			cardRing(gtx, r, dp(gtx, 8), cl.Ring, cl.Bg)
			rrect(gtx, r, dp(gtx, 8), cl.Bg)
			img := pixeltest.Render(t, w, h, &ops)

			full := int(math.Floor(float64(scale) + 1e-4))
			frac := float64(scale) - float64(full)
			want := []color.NRGBA{} // outward from the card's edge
			for range full {
				want = append(want, chromiumRingPixel(css, bg, 1))
			}
			want = append(want, chromiumRingPixel(css, bg, frac), bg)
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			for i, c := range want {
				if got := img.RGBAAt(r.Min.X-1-i, 70); !within(got, c, 2) {
					t.Errorf("%s %vx: left edge, device px %d out: %v, want %v (Chromium: %d whole px, then %.2f covered)", mode, scale, i+1, got, c, full, frac)
				}
				if got := img.RGBAAt(100, r.Min.Y-1-i); !within(got, c, 2) {
					t.Errorf("%s %vx: top edge, device px %d out: %v, want %v", mode, scale, i+1, got, c)
				}
				if got := img.RGBAAt(r.Max.X+i, 70); !within(got, c, 2) {
					t.Errorf("%s %vx: right edge, device px %d out: %v, want %v", mode, scale, i+1, got, c)
				}
			}
			if got := img.RGBAAt(r.Min.X, 70); !within(got, bg, 1) {
				t.Errorf("%s %vx: the ring reaches into the card: %v", mode, scale, got)
			}
		}
	}
}

// The popover cards draw that ring: styledCardLook with no rule and the
// square-topped search menus at 1.25x, and no card in popups, slackc/kit,
// blockkit or composer paints its ring as one device pixel any more
// (r.Inset(-1) under the card's Ring).
func TestPopoverCardsUseTheRing(t *testing.T) {
	cl := colorsOf(theme.New(false))
	const scale = 1.25
	partial := chromiumRingPixel(cssRingColor(false), cl.Bg, .25)
	for _, c := range []struct {
		name string
		draw func(gtx layout.Context, r image.Rectangle)
	}{
		{"styledCardLook", func(gtx layout.Context, r image.Rectangle) {
			lk := cardLook{fill: cl.Bg, ring: cl.Ring, shadow: func(layout.Context, image.Rectangle, int) {}}
			styledCardLook(gtx, slackc.Styled{}, lk, r.Min, r.Size(), dp(gtx, 8), func(layout.Context) {}) // no rule: the native card
		}},
		{"square-topped", func(gtx layout.Context, r image.Rectangle) {
			cardRingCorners(gtx, r, [4]int{0, 0, dp(gtx, 8), dp(gtx, 8)}, cl.Ring, cl.Bg)
		}},
	} {
		const w, h = 200, 140
		var ops op.Ops
		paint.FillShape(&ops, cl.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Now: time.Now(),
			Constraints: layout.Exact(image.Pt(w, h))}
		r := image.Rect(40, 40, 160, 100)
		c.draw(gtx, r)
		img := pixeltest.Render(t, w, h, &ops)
		if got := img.RGBAAt(100, r.Min.Y-2); !within(got, partial, 3) {
			t.Errorf("%s at 1.25x: 2 device px above the top edge %v, want the quarter-covered ring %v", c.name, got, partial)
		}
	}
	for _, glob := range []string{"*.go", "../slackc/kit/*.go", "../blockkit/*.go", "../composer/*.go"} {
		files, err := filepath.Glob(glob)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for i, l := range strings.Split(string(b), "\n") {
				if strings.Contains(l, ".Inset(-1)") && strings.Contains(l, "Ring") {
					t.Errorf("%s:%d paints a card ring as one device pixel (use cardRing / CardRing): %s", f, i+1, strings.TrimSpace(l))
				}
			}
		}
	}
}

func TestCardRingShots(t *testing.T) {
	dir := os.Getenv("BORDER_SHOTS_DIR")
	if dir == "" {
		t.Skip("BORDER_SHOTS_DIR not set")
	}
	sh := theme.NewShaper()
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th := theme.New(dark)
		th.SetShapers(sh, sh)
		cl := colorsOf(th)
		for _, scale := range []float32{1.25, 2} {
			for _, after := range []bool{false, true} {
				cssBorders = after
				w, h := int(260*scale), int(150*scale)
				var ops op.Ops
				paint.FillShape(&ops, cl.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
				gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Now: time.Now(),
					Constraints: layout.Exact(image.Pt(w, h))}
				r := image.Rect(dp(gtx, 24), dp(gtx, 20), dp(gtx, 236), dp(gtx, 120))
				card(gtx, cl, r, dp(gtx, 8)) // .c-menu: box-shadow 0 0 0 1px, 0 4px 12px
				for i, s := range []string{"Copy link", "Mark unread", "Remind me about this"} {
					at(gtx, r.Min.Add(image.Pt(dp(gtx, 24), dp(gtx, 12+28*float32(i)))), func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Fg, s)
					})
				}
				img := pixeltest.Render(t, w, h, &ops)
				name := "card-ring-" + mode + "-" + map[float32]string{1.25: "125", 2: "200"}[scale] + "-" +
					map[bool]string{false: "before", true: "after"}[after]
				save := func(name string, m image.Image) {
					f, err := os.Create(filepath.Join(dir, name+".png"))
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					if err := png.Encode(f, m); err != nil {
						t.Fatal(err)
					}
				}
				save(name, img)
				// the top-left corner, 8x (nearest): the ring's pixels
				crop := image.Rect(r.Min.X-6, r.Min.Y-6, r.Min.X+18, r.Min.Y+18)
				zoom := image.NewRGBA(image.Rect(0, 0, crop.Dx()*8, crop.Dy()*8))
				for y := range zoom.Bounds().Dy() {
					for x := range zoom.Bounds().Dx() {
						zoom.Set(x, y, img.At(crop.Min.X+x/8, crop.Min.Y+y/8))
					}
				}
				save(name+"-corner8x", zoom)
			}
		}
	}
	cssBorders = true
}
