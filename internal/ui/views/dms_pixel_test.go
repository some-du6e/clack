package views

import (
	"context"
	"fmt"
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
		// the page's column is the sidebar's scope (LayoutList sets
		// Chrome; TestDMsListChromeScope), the card the root's
		prev := d.th.Chrome
		d.th.Chrome = !peek
		defer func() { d.th.Chrome = prev }()
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

// drawnInk is text ink c of weight w as Clack's labels paint it over bg
// in the chrome scope or not: theme.LabelInk's light-text fade (light
// text on dark, as thin as Chrome's), blended in linear light as Gio does.
func drawnInk(c, bg color.NRGBA, w font.Weight, chrome bool) color.NRGBA {
	c = theme.LabelInk(c, 1, w, chrome)
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

// inkSum is the ink in r: every pixel's luma distance from bg's (sRGB,
// as text_render_compare.py counts ink; linear light would squeeze dark
// greys on white together), so two drawings of the same text in two
// colours or weights compare whatever their glyphs' coverage.
func inkSum(img *image.RGBA, r image.Rectangle, bg color.NRGBA) float64 {
	lum := func(r, g, b uint8) float64 { return 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b) }
	b := lum(bg.R, bg.G, bg.B)
	var sum float64
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			sum += math.Abs(lum(c.R, c.G, c.B) - b)
		}
	}
	return sum
}

// dmsTokens are Slack's tokens for one case: the card's (the root scope,
// testdata/theme-oracle.json) and the page column's (the sidebar's,
// TestDMsListFollowsSidebarScope's table).
type dmsTokens struct {
	invPry, invSec, invTer, rule, hover color.NRGBA // the page: theme-content-inv-*, otl-inv-pry, surf-inv-ter (light)
	cardBg, cardPry, cardSec            color.NRGBA // the card: ctr-pry, content-pry, content-sec
	cardInvSec, cardInvTer, cardRule    color.NRGBA // the card: theme-content-inv-sec / -inv-ter, theme-otl-inv-pry
}

// The DMs rows as drawn, on the peek card and on the page, light and dark,
// Slack's default theme and Karim's, activity_inbox on (Karim's account)
// and off: one DM drawn read, unread and muted with unread messages.
//
//   - ActivityMessage: with the inbox on the preview has no state class
//     and takes the item's colour (the page's theme-content-inv-pry, the
//     card's content-sec, the card being a portal outside .p-ia4_client),
//     the same for all three; with it off --muted (theme-content-inv-sec
//     on the page, content-sec on the card) beats --read and --unread
//     (theme-content-inv-pry on the page, content-pry on the card).
//   - DmsChannel: an unread name is --unread (900), a muted one --muted
//     (--dt_color-content-ter, 700); the timestamp is
//     theme-content-inv-sec, bold while unread and not muted, and a
//     muted one's theme-content-inv-ter.
//   - A row's border-bottom is theme-otl-inv-pry in its scope (the card's
//     root one; Clack drew sk_foreground_low there).
//   - Hovered, the card's row is base-pry under base-pry-hover in light
//     mode, base-pry-hover alone on the card in dark mode (#323538); the
//     page has theme-surf-inv-ter in light mode and no tile in dark mode
//     (.p-dms_list--dark), and the presence ring's cutout is the tile, or
//     the column, under it.
func TestDMsRowsDrawn(t *testing.T) {
	app := dmsFake(t)
	hx := func(v uint32) color.NRGBA { return rgb(v) }
	hxa := func(v uint32, a uint8) color.NRGBA { c := rgb(v); c.A = a; return c }
	lightCard := dmsTokens{cardBg: hx(0xffffff), cardPry: hx(0x1d1c1d), cardSec: hx(0x454447)}
	darkCard := dmsTokens{cardBg: hx(0x212428), cardPry: hx(0xf8f8f8), cardSec: hx(0xb9babd)}
	with := func(base dmsTokens, f func(*dmsTokens)) dmsTokens { f(&base); return base }
	for _, c := range []struct {
		name, ia string
		dark     bool
		tok      dmsTokens
	}{
		{"default light", ``, false, with(lightCard, func(k *dmsTokens) {
			k.invPry, k.invSec, k.invTer, k.rule, k.hover = hx(0xffffff), hxa(0xf6e4ff, 0xe6), hxa(0xf9edff, 0x99), hxa(0xf9edff, 0x38), hxa(0xf9edff, 0x14)
			k.cardInvSec, k.cardInvTer, k.cardRule = hxa(0x39063a, 0xcc), hxa(0x39063a, 0x80), hxa(0x611f69, 0x21)
		})},
		{"default dark", ``, true, with(darkCard, func(k *dmsTokens) {
			k.invPry, k.invSec, k.invTer, k.rule = hx(0xf8f8f8), hxa(0xe3ceeb, 0xcc), hxa(0xe3ceeb, 0x99), hxa(0xefe1f5, 0x38)
			k.cardInvSec, k.cardInvTer, k.cardRule = hxa(0xe3ceeb, 0xcc), hxa(0xe3ceeb, 0x99), hxa(0xefe1f5, 0x38)
		})},
		{"karim import light", importIA, false, with(lightCard, func(k *dmsTokens) {
			k.invPry, k.invSec, k.invTer, k.rule, k.hover = hx(0x1c0901), hxa(0x1c0901, 0xcc), hxa(0x1c0901, 0x80), hxa(0x41281a, 0x21), hxa(0x41281a, 0x21)
			k.cardInvSec, k.cardInvTer, k.cardRule = hxa(0x1c0901, 0xcc), hxa(0x1c0901, 0x80), hxa(0x41281a, 0x21)
		})},
		{"karim picker dark", pickerIA, true, with(darkCard, func(k *dmsTokens) {
			k.invPry, k.invSec, k.invTer, k.rule = hx(0xf8f8f8), hxa(0xffdfcf, 0xcc), hxa(0xffdfcf, 0x99), hxa(0xfff4f0, 0x38)
			k.cardInvSec, k.cardInvTer, k.cardRule = hxa(0xffdfcf, 0xcc), hxa(0xffdfcf, 0x99), hxa(0xfff4f0, 0x38)
		})},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newKBHost(app, c.dark)
			h.th = fullThemed(c.ia, c.dark)
			theme.SetPrimary(h.th) // the app's theme: the text model follows its mode
			defer theme.SetPrimary(nil)
			d := NewDMs(h)
			e := &vaEnv{size: image.Pt(900, 600)}
			e.draw = func(gtx layout.Context) { d.Layout(gtx) }
			e.until(t, "the DMs", func() bool { return len(d.items) > 0 && d.items[0].Latest != nil })
			d.sel = "" // the page preselects a DM: these rows draw unselected
			read := d.items[0]
			read.Count, read.Conv.Muted = core.Count{}, false
			unread := read
			unread.Count = core.Count{Unread: true}
			muted := unread
			muted.Conv.Muted = true
			// a presence dot, so the ring's cutout draws
			if u, ok := app.S.User(read.Conv.User); ok {
				cp := *u
				cp.Presence = "active"
				app.S.PutUsers([]slack.User{cp})
			}
			contentTer := hx(0x5e5d60)
			if c.dark {
				contentTer = hx(0x9a9b9e)
			}
			k := c.tok
			for _, inbox := range []bool{true, false} {
				app.SetExperimentGroup("activity_inbox", map[bool]string{true: "on", false: "off"}[inbox])
				for _, peek := range []bool{true, false} {
					where := fmt.Sprintf("inbox=%v page", inbox)
					if peek {
						where = fmt.Sprintf("inbox=%v peek card", inbox)
					}
					imgR, sz := dmsRowShot(t, d, &read, peek, nil)
					imgU, _ := dmsRowShot(t, d, &unread, peek, nil)
					imgM, _ := dmsRowShot(t, d, &muted, peek, nil)
					d.peeking = peek
					d.dmsColors() // the background the rows were drawn over
					d.peeking = false
					bg := d.c.chrome
					// Slack's colours, flattened over the background as Slack
					// blends them (sRGB)
					namePry, time, ter, rule := flatten(k.invPry, bg), flatten(k.invSec, bg), flatten(k.invTer, bg), flatten(k.rule, bg)
					prevRead, prevMuted := namePry, flatten(k.invSec, bg)
					if inbox {
						prevMuted = namePry
					}
					if peek {
						if bg != k.cardBg {
							t.Fatalf("%s: the card is %s, Slack's ctr-pry %s", where, hexA(bg), hexA(k.cardBg))
						}
						namePry, time, ter, rule = k.cardPry, flatten(k.cardInvSec, bg), flatten(k.cardInvTer, bg), flatten(k.cardRule, bg)
						prevRead, prevMuted = k.cardPry, k.cardSec
						if inbox {
							prevRead = k.cardSec
						}
					}
					w := sz.X
					nameR, prevR, timeR := image.Rect(60, 16, 200, 38), image.Rect(60, 40, 260, 58), image.Rect(w-16-60, 16, w-16, 38)
					// the inks Slack draws, as Clack's labels paint them
					for _, f := range []struct {
						what      string
						img       *image.RGBA
						r         image.Rectangle
						want      color.NRGBA
						wt        font.Weight
						aMin, aMx float64
					}{
						{"read preview", imgR, prevR, prevRead, wReg, .7, 1.1},
						{"muted preview", imgM, prevR, prevMuted, wReg, .7, 1.1},
						{"read name (theme-content-inv-pry / content-pry)", imgR, nameR, namePry, wBold, .85, 1.08},
						{"muted name (--dt_color-content-ter)", imgM, nameR, contentTer, wBold, .85, 1.08},
						{"read timestamp (theme-content-inv-sec)", imgR, timeR, time, wReg, .7, 1.12},
						{"muted timestamp (theme-content-inv-ter)", imgM, timeR, ter, wReg, .7, 1.12},
					} {
						ink := drawnInk(f.want, bg, f.wt, !peek)
						if a, off, peak := inkAlong(f.img, f.r, bg, ink); a < f.aMin || a > f.aMx || off > 16 {
							t.Errorf("%s: the %s peaks at %s (%.2f of the way from %s to %s, Slack's %s as Clack's labels ink it; %.0f off it)", where, f.what, hexA(peak), a, hexA(bg), hexA(ink), hexA(f.want), off)
						}
					}
					// the same text in three states
					ratio := func(a, b *image.RGBA, r image.Rectangle) float64 { return inkSum(a, r, bg) / inkSum(b, r, bg) }
					if q := ratio(imgU, imgR, prevR); math.Abs(q-1) > 0.03 {
						t.Errorf("%s: an unread preview has %.2f× a read one's ink; Slack draws them alike", where, q)
					}
					switch q := ratio(imgM, imgR, prevR); {
					case inbox && !peek && math.Abs(q-1) > 0.03:
						t.Errorf("%s: a muted preview has %.2f× a read one's ink; with activity_inbox Slack draws every preview in the item's colour", where, q)
					case inbox && peek && math.Abs(q-1) > 0.03:
						t.Errorf("%s: a muted preview has %.2f× a read one's ink; Slack's card draws both content-sec", where, q)
					case !inbox && q > 0.93:
						t.Errorf("%s: a muted preview has %.2f× a read one's ink; Slack's --muted is dimmer", where, q)
					}
					if q := ratio(imgU, imgR, nameR); q < 1.08 {
						t.Errorf("%s: an unread name has %.2f× a read one's ink; Slack's --unread is 900 against 700", where, q)
					}
					if q := ratio(imgU, imgR, timeR); q < 1.1 {
						t.Errorf("%s: an unread timestamp has %.2f× a read one's ink; Slack's --unread is bold", where, q)
					}
					// the row's rule: its last row of pixels
					if got := imgR.RGBAAt(w/2, sz.Y-1); !nearPx(got, rule, 2) {
						t.Errorf("%s: the row's rule is %s; Slack theme-otl-inv-pry %s over %s = %s", where, pxHex(got), hexA(k.rule), hexA(bg), hexA(rule))
					}
					// the :hover tile at the row's bottom left (clear of the
					// avatar, the action bar and the rule), and the presence
					// ring's cutout over it (the badge 32px into the avatar)
					probe, ringPx, dotPx := image.Pt(4, sz.Y-4), image.Pt(16+32+6, 16+32), image.Pt(16+32, 16+32)
					if got := imgR.RGBAAt(probe.X, probe.Y); !nearPx(got, bg, 1) {
						t.Errorf("%s: an idle row's fill %s, the list's %s", where, pxHex(got), hexA(bg))
					}
					ptr := image.Pt(200, 30)
					imgH, _ := dmsRowShot(t, d, &read, peek, &ptr)
					var hov color.NRGBA
					switch {
					case peek && c.dark:
						hov = hx(0x323538) // #f8f8f8 at .08 over the card's #212428
					case peek:
						hov = flatten(argb(0x0f454447), hx(0xffffff))
					case c.dark:
						hov = bg // .p-dms_list--dark: no tile
					default:
						hov = flatten(k.hover, bg) // theme-surf-inv-ter
					}
					if got := imgH.RGBAAt(probe.X, probe.Y); !nearPx(got, hov, 1) {
						t.Errorf("%s: a hovered row's fill %s, Slack %s", where, pxHex(got), hexA(hov))
					}
					if got := imgH.RGBAAt(dotPx.X, dotPx.Y); !nearPx(got, d.c.green, 3) {
						t.Errorf("%s: no presence dot at %v (%s, want %s)", where, dotPx, pxHex(got), hexA(d.c.green))
					} else if got := imgH.RGBAAt(ringPx.X, ringPx.Y); !nearPx(got, hov, 1) {
						t.Errorf("%s: the hovered row's presence ring is %s over its %s", where, pxHex(got), hexA(hov))
					}
				}
			}
			app.SetExperimentGroup("activity_inbox", "on")
			if c.dark {
				// a directory person (Find a DM's People) hovered on the dark
				// page: no tile, so the ring's cutout is the column
				u, ok := h.Core().User(read.Conv.User)
				if !ok {
					t.Fatalf("no member %s", read.Conv.User)
				}
				img := dmsPersonShot(t, d, u, image.Pt(200, 30))
				d.dmsColors()
				ring, dot := img.RGBAAt(16+32+6, 16+32), img.RGBAAt(16+32, 16+32)
				if !nearPx(dot, d.c.green, 3) {
					t.Errorf("a directory person has no presence dot (%s)", pxHex(dot))
				} else if !nearPx(ring, d.c.chrome, 1) {
					t.Errorf("a hovered directory person's presence ring is %s on the dark page's %s (no tile there)", pxHex(ring), hexA(d.c.chrome))
				}
			}
		})
	}
}

// dmsPersonShot draws d's directory row for u on the page with the
// pointer at ptr for two frames, and renders the last.
func dmsPersonShot(t *testing.T, d *DMs, u core.User, ptr image.Point) *image.RGBA {
	t.Helper()
	e := &vaEnv{size: image.Pt(420, 120)}
	e.draw = func(gtx layout.Context) {
		d.frame()
		d.dmsColors()
		prev := d.th.Chrome
		d.th.Chrome = true
		defer func() { d.th.Chrome = prev }()
		fill(gtx, image.Rectangle{Max: e.size}, d.c.chrome)
		gtx.Constraints.Min = image.Point{}
		d.personRow(gtx, u)
	}
	e.frame()
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(ptr.X), float32(ptr.Y))})
	e.frame()
	e.frame()
	return pixeltest.Render(t, e.size.X, e.size.Y, &e.ops)
}

// The DMs page's list column is the sidebar's theme scope, so its labels
// draw under the chrome's light-text calibration (Theme.Chrome), as the
// sidebar's do; the card's don't.
func TestDMsListChromeScope(t *testing.T) {
	app := dmsFake(t)
	h := newKBHost(app, true)
	h.th = fullThemed("", true)
	d := NewDMs(h)
	e := &vaEnv{size: image.Pt(900, 600)}
	var header, card []bool
	prev := theme.DrawHook
	defer func() { theme.DrawHook = prev }()
	theme.DrawHook = func(txt string) {
		if txt == nsRecent.T("Direct messages") { // layoutHeader's title
			header = append(header, h.th.Chrome)
		}
	}
	e.draw = func(gtx layout.Context) { d.Layout(gtx) }
	e.until(t, "the DMs", func() bool { return len(d.items) > 0 })
	e.frame()
	if len(header) == 0 || !header[len(header)-1] {
		t.Errorf("the DMs page's header drew with Chrome %v; the column is the sidebar's scope", header)
	}
	if h.th.Chrome {
		t.Error("Chrome stays set after the page's frame")
	}
	theme.DrawHook = func(string) { card = append(card, h.th.Chrome) } // every label the card draws
	e.draw = func(gtx layout.Context) { d.LayoutPeek(gtx) }
	e.frame()
	e.frame()
	if len(card) == 0 {
		t.Fatal("the DMs card drew no labels")
	}
	for _, ch := range card {
		if ch {
			t.Error("the DMs card drew labels in the chrome scope; the card is the root's")
			break
		}
	}
}
