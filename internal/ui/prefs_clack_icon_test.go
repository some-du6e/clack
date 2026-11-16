package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestPrefsClackNavIcon draws Preferences > Clack's nav row (the
// monochrome clack-logo) between its neighbours, Advanced (settings, the
// last of Slack's tabs) and Plugins (apps), unselected and selected, in
// light and dark, at 2x, and checks that the logo is drawn in the colour
// the nav gives its other icons (its most-inked pixel matches theirs) and
// at their size (its ink box lies within theirs, give or take 2px).
// PREFS_AUDIT_SHOTS_DIR gets prefs-clack-nav-{light,dark}.png.
func TestPrefsClackNavIcon(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	if clackNav.icon != "clack-logo" {
		t.Fatalf("clackNav icon %q, want clack-logo", clackNav.icon)
	}
	const k = 2 // px per dp
	const navW = 220
	rows := []struct{ id, label, icon string }{
		{"advanced", "Advanced", "settings"},
		{clackTabID, AppName, clackNav.icon},
		{pluginsTabID, "Plugins", "apps"},
	}
	rowH := 29 * k
	size := image.Pt(navW*k, 2*len(rows)*rowH+8*k)
	dir := os.Getenv("PREFS_AUDIT_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		a.th.Dark = dark
		a.th.Refresh()
		pc := p.colors()
		bg := pc.field // the dialog's background: white, #1a1d21
		// the rows unselected, then each selected (its own block)
		ops := new(op.Ops)
		gtx := layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: k, PxPerSp: k}, Constraints: layout.Exact(size)}
		a.slackcFrame(gtx)
		paint.FillShape(ops, bg, clip.Rect{Max: size}.Op())
		y := func(block, i int) int { return (block*len(rows)+i)*rowH + block*8*k }
		for block := range 2 {
			for i, r := range rows {
				p.navItem(gtx, y(block, i), navW*k, r.id, r.label, nil, r.icon, block == 1)
			}
		}
		img := pixeltest.Render(t, size.X, size.Y, ops)
		if dir != "" {
			f, err := os.Create(filepath.Join(dir, "prefs-clack-nav-"+mode+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
		for block, sel := range []string{"unselected", "selected"} {
			type glyph struct {
				box  image.Rectangle
				peak color.RGBA
			}
			var gs []glyph
			for i, r := range rows {
				y0 := y(block, i)
				// the row's own background, left of the icon (9dp in)
				rbg := img.RGBAAt(4*k, y0+14*k)
				g := glyph{peak: rbg}
				best := 0
				cell := image.Rect(9*k, y0+7*k, 24*k, y0+22*k)
				for py := cell.Min.Y; py < cell.Max.Y; py++ {
					for px := cell.Min.X; px < cell.Max.X; px++ {
						c := img.RGBAAt(px, py)
						d := absDiff(c.R, rbg.R) + absDiff(c.G, rbg.G) + absDiff(c.B, rbg.B)
						if d > 60 {
							g.box = g.box.Union(image.Rect(px, py, px+1, py+1))
						}
						if d > best {
							best, g.peak = d, c
						}
					}
				}
				// nothing past the 15dp icon cell, above or below it
				for _, out := range []image.Rectangle{image.Rect(9*k-2, y0+7*k-2, 24*k+2, y0+7*k), image.Rect(9*k-2, y0+22*k, 24*k+2, y0+22*k+2)} {
					if pt, ok := navInkIn(img, out, rbg); ok {
						t.Errorf("%s %s %s: ink at %v, outside the icon cell", mode, sel, r.id, pt)
					}
				}
				if g.box.Empty() {
					t.Fatalf("%s %s %s: no icon drawn", mode, sel, r.id)
				}
				t.Logf("%s %s %-9s box %v (%dx%d) peak %v on %v", mode, sel, r.id, g.box, g.box.Dx(), g.box.Dy(), g.peak, rbg)
				gs = append(gs, g)
			}
			logo, nb := gs[1], []glyph{gs[0], gs[2]}
			for j, n := range nb {
				if d := absDiff(logo.peak.R, n.peak.R) + absDiff(logo.peak.G, n.peak.G) + absDiff(logo.peak.B, n.peak.B); d > 24 {
					t.Errorf("%s %s: the logo's ink %v, %s's %v", mode, sel, logo.peak, rows[[]int{0, 2}[j]].id, n.peak)
				}
			}
			lo := func(f func(image.Rectangle) int) int { return min(f(nb[0].box), f(nb[1].box)) - 2 }
			hi := func(f func(image.Rectangle) int) int { return max(f(nb[0].box), f(nb[1].box)) + 2 }
			dx := func(r image.Rectangle) int { return r.Dx() }
			dy := func(r image.Rectangle) int { return r.Dy() }
			if w := logo.box.Dx(); w < lo(dx) || w > hi(dx) {
				t.Errorf("%s %s: the logo is %dpx wide, its neighbours %d and %d", mode, sel, w, nb[0].box.Dx(), nb[1].box.Dx())
			}
			if h := logo.box.Dy(); h < lo(dy) || h > hi(dy) {
				t.Errorf("%s %s: the logo is %dpx tall, its neighbours %d and %d", mode, sel, h, nb[0].box.Dy(), nb[1].box.Dy())
			}
			// centred in the cell as they are (±1.5px of the cell's centre)
			cx, cy := logo.box.Min.X+logo.box.Max.X-2*16*k-k, logo.box.Min.Y+logo.box.Max.Y-2*(y(block, 1)+14*k)-k
			if cx < -3 || cx > 3 || cy < -3 || cy > 3 {
				t.Errorf("%s %s: the logo's ink box %v is off the icon cell's centre (2×offset %d,%d)", mode, sel, logo.box, cx, cy)
			}
		}
	}
	a.th.Dark = false
	a.th.Refresh()
}

// navInkIn reports the first pixel in r that isn't (near) bg.
func navInkIn(img *image.RGBA, r image.Rectangle, bg color.RGBA) (image.Point, bool) {
	for py := r.Min.Y; py < r.Max.Y; py++ {
		for px := r.Min.X; px < r.Max.X; px++ {
			c := img.RGBAAt(px, py)
			if absDiff(c.R, bg.R)+absDiff(c.G, bg.G)+absDiff(c.B, bg.B) > 60 {
				return image.Pt(px, py), true
			}
		}
	}
	return image.Point{}, false
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
