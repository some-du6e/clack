package views

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// dmsFake boots a core on the views fixtures with client.dms' DMs
// (Size.DMsTab: the DMs page's own list).
func dmsFake(t *testing.T) *core.App {
	t.Helper()
	sz := fakeslack.Small
	sz.Views, sz.DMsTab = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	if err := app.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	return app
}

// fullThemed is themed with a text shaper (theme.New), for drawing.
func fullThemed(ia string, dark bool) *theme.Theme {
	th := theme.New(dark)
	if t, ok := theme.ParseIATheme(ia); ok {
		th.Restyle = func(th *theme.Theme, p *theme.Palette) { theme.ApplyIATheme(p, t, th.Dark, true) }
		th.Refresh()
	}
	return th
}

// dmsRowShot draws d's row for dm (on the peek card when peek) at the top
// of a 420x120 frame over the list's own colour, with the pointer at ptr
// (nil: under the row) for two frames, and renders the last one. It needs
// a GPU: CI runs it with CLACK_PIXEL_TESTS=1 (prefs-colors.yml). It
// returns the row's size.
func dmsRowShot(t *testing.T, d *DMs, dm *core.DM, peek bool, ptr *image.Point) (*image.RGBA, image.Point) {
	t.Helper()
	e := &vaEnv{size: image.Pt(420, 120)}
	var size image.Point
	e.draw = func(gtx layout.Context) {
		d.peeking = peek
		defer func() { d.peeking = false }()
		d.frame()
		d.dmsColors()
		fill(gtx, image.Rectangle{Max: e.size}, d.c.chrome)
		gtx.Constraints.Min = image.Point{}
		r := d.rowState(dm.Channel)
		r.v, r.dm = d, dm
		size = d.dmRow(gtx, dm).Size
	}
	at := image.Pt(e.size.X/2, e.size.Y-2)
	if ptr != nil {
		at = *ptr
	}
	e.frame()
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(at.X), float32(at.Y))})
	e.frame()
	e.frame()
	return pixeltest.Render(t, e.size.X, e.size.Y, &e.ops), size
}

// inkAlong is the most-covered ink pixel in r (the one farthest from bg),
// placed on the line from bg to ink: a is how far along it lies (1: ink
// itself, 0: none drawn, past 1: a stronger ink than want) and off how far
// it is from that line, in levels.
func inkAlong(img *image.RGBA, r image.Rectangle, bg, ink color.NRGBA) (a, off float64, peak color.NRGBA) {
	far := -1
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			d := iabs(int(c.R)-int(bg.R)) + iabs(int(c.G)-int(bg.G)) + iabs(int(c.B)-int(bg.B))
			if d > far {
				far, peak = d, color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff}
			}
		}
	}
	dv := [3]float64{float64(ink.R) - float64(bg.R), float64(ink.G) - float64(bg.G), float64(ink.B) - float64(bg.B)}
	pv := [3]float64{float64(peak.R) - float64(bg.R), float64(peak.G) - float64(bg.G), float64(peak.B) - float64(bg.B)}
	dd := dv[0]*dv[0] + dv[1]*dv[1] + dv[2]*dv[2]
	if dd == 0 {
		return 0, 0, peak
	}
	a = (pv[0]*dv[0] + pv[1]*dv[1] + pv[2]*dv[2]) / dd
	var res float64
	for i := range pv {
		e := pv[i] - a*dv[i]
		res += e * e
	}
	return a, math.Sqrt(res), peak
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// drawnInk is text ink c of weight w as Clack's labels paint it over bg:
// theme.LabelInk's light-text fade (light text on dark, as thin as
// Chrome's), blended in linear light as Gio does.
func drawnInk(c, bg color.NRGBA, w font.Weight) color.NRGBA {
	c = theme.LabelInk(c, 1, w, false)
	a := float64(c.A) / 255
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	srgb := func(l float64) uint8 {
		x := 12.92 * l
		if l > 0.0031308 {
			x = 1.055*math.Pow(l, 1/2.4) - 0.055
		}
		return uint8(math.Round(math.Max(0, math.Min(1, x)) * 255))
	}
	mix := func(f, b uint8) uint8 { return srgb(lin(f)*a + lin(b)*(1-a)) }
	return color.NRGBA{R: mix(c.R, bg.R), G: mix(c.G, bg.G), B: mix(c.B, bg.B), A: 0xff}
}

// nearPx reports whether the drawn pixel is within tol per channel of c.
func nearPx(got color.RGBA, c color.NRGBA, tol int) bool {
	return iabs(int(got.R)-int(c.R)) <= tol && iabs(int(got.G)-int(c.G)) <= tol && iabs(int(got.B)-int(c.B)) <= tol
}

func pxHex(c color.RGBA) string { return hexA(color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff}) }

// The DMs rows as drawn, on the peek card and on the page, light and dark,
// Slack's default theme and Karim's: a muted conversation with unread
// messages (DmsChannel's p-dms_channel__name--muted and
// p-dms_channel__timestamp--muted, ActivityMessage's __message--muted
// before --unread) and the rows' :hover tile.
//
//   - Its timestamp is theme-content-inv-ter: the peek card's root scope
//     (Slack's tokens, testdata/theme-oracle.json) or the sidebar's on the
//     page. Before, the card drew it at alpha 0.
//   - Its name is --dt_color-content-ter (#5e5d60, #9a9b9e), not the
//     list's name ink; its preview the muted theme-content-inv-sec, not
//     the unread one's ink.
//   - Hovered, the card's row is base-pry under base-pry-hover in light
//     mode, the base-pry-hover image alone on the card in dark mode
//     (#323538); the page has the theme-surf-inv-ter tile in light mode
//     and none in dark mode (.p-dms_list--dark).
func TestDMsRowsDrawn(t *testing.T) {
	app := dmsFake(t)
	hxa := func(v uint32, a uint8) color.NRGBA { c := rgb(v); c.A = a; return c }
	for _, c := range []struct {
		name, ia string
		dark     bool
		invTer   color.NRGBA // the root scope's theme-content-inv-ter (the card)
	}{
		{"default light", ``, false, hxa(0x39063a, 0x80)},
		{"default dark", ``, true, hxa(0xe3ceeb, 0x99)},
		{"karim import light", importIA, false, hxa(0x1c0901, 0x80)},
		{"karim picker dark", pickerIA, true, hxa(0xffdfcf, 0x99)},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newKBHost(app, c.dark)
			h.th = fullThemed(c.ia, c.dark)
			d := NewDMs(h)
			e := &vaEnv{size: image.Pt(900, 600)}
			e.draw = func(gtx layout.Context) { d.Layout(gtx) }
			e.until(t, "the DMs", func() bool { return len(d.items) > 1 && d.items[0].Latest != nil && d.items[1].Latest != nil })
			d.sel = "" // the page preselects a DM: these rows draw unselected
			muted := d.items[0]
			muted.Conv.Muted = true
			muted.Count = core.Count{Unread: true}
			plain := d.items[1]
			plain.Count = core.Count{}
			contentTer := rgb(0x5e5d60)
			if c.dark {
				contentTer = rgb(0x9a9b9e)
			}
			for _, peek := range []bool{true, false} {
				where := "page"
				if peek {
					where = "peek card"
				}
				img, sz := dmsRowShot(t, d, &muted, peek, nil)
				d.peeking = peek
				d.dmsColors() // the colours the row was drawn over and with
				d.peeking = false
				bg, ter, name, soft, white := d.c.chrome, d.c.ter, contentTer, d.c.soft, d.c.white
				if peek {
					ter = flatten(c.invTer, bg) // Slack's own value on the card
				}
				w := sz.X
				nameR, prevR := image.Rect(60, 16, 140, 38), image.Rect(60, 40, 210, 58)
				for _, f := range []struct {
					what      string
					r         image.Rectangle
					want      color.NRGBA
					wt        font.Weight
					aMin, aMx float64
				}{
					{"muted timestamp (theme-content-inv-ter)", image.Rect(w-16-60, 16, w-16, 38), ter, wReg, .7, 1.12},
					{"muted name (--dt_color-content-ter)", nameR, name, wBold, .85, 1.08},
					{"muted, unread preview (theme-content-inv-sec)", prevR, soft, wReg, .7, 1.1},
				} {
					ink := drawnInk(f.want, bg, f.wt)
					a, off, peak := inkAlong(img, f.r, bg, ink)
					if a < f.aMin || a > f.aMx || off > 16 {
						t.Errorf("%s: %s peaks at %s (%.2f of the way from %s to %s, Slack's %s as Clack's labels ink it; %.0f off it)", where, f.what, hexA(peak), a, hexA(bg), hexA(ink), hexA(f.want), off)
					}
				}
				// the name and the preview don't take the unmuted / unread ink
				for _, f := range []struct {
					r  image.Rectangle
					wt font.Weight
				}{{nameR, wBold}, {prevR, wReg}} {
					if white != name && white != soft {
						ink := drawnInk(white, bg, f.wt)
						if a, _, peak := inkAlong(img, f.r, bg, ink); a > .92 {
							t.Errorf("%s: %v draws %s, the unmuted ink %s", where, f.r, hexA(peak), hexA(ink))
						}
					}
				}
				// the :hover tile, at the row's bottom left (clear of the
				// avatar and the action bar)
				probe := image.Pt(4, sz.Y-4)
				if got := img.RGBAAt(probe.X, probe.Y); !nearPx(got, bg, 1) {
					t.Errorf("%s: an idle row's fill %s, the list's %s", where, pxHex(got), hexA(bg))
				}
				ptr := image.Pt(200, 30)
				img, psz := dmsRowShot(t, d, &plain, peek, &ptr)
				probe = image.Pt(4, psz.Y-4)
				var hov color.NRGBA
				switch {
				case peek && c.dark:
					hov = rgb(0x323538) // #f8f8f8 at .08 over the card's #212428
				case peek:
					hov = flatten(argb(0x0f454447), rgb(0xffffff))
				case c.dark:
					hov = bg // .p-dms_list--dark: no tile
				default:
					hov = d.c.tint // theme-surf-inv-ter
				}
				if got := img.RGBAAt(probe.X, probe.Y); !nearPx(got, hov, 1) {
					t.Errorf("%s: a hovered row's fill %s, Slack %s", where, pxHex(got), hexA(hov))
				}
			}
		})
	}
}
