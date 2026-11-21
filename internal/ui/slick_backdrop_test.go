package ui

// The app behind Preferences (ui-slick workflow, real upstream themes):
// Karim, 2026-11-21, Verdant Circuit on Preferences > Themes: "intended?",
// a window of flat #0d1810 with nothing drawn behind the dialog.
//
// Slack draws a modal's scrim as .c-sk-overlay:before{background-color:
// var(--dt_color-base-modal)} (client-boot-styles) over the client, which
// stays drawn. Verdant Circuit sets --dt_color-base-modal to
// rgba(13,24,16,0.98), so Slack shows the client through it at 2%: dark,
// but there (its bright text and the sidebar still read as shapes). Clack
// corrected that alpha for linear blending against the theme's #101d14
// card, where the 8-bit blend rounds to the scrim itself, and drew it
// opaque (theme.LinearAlpha). Slick's Themes tab keeps the scrim: Slick
// tells the dialog it is on "advanced". Slack's own Appearance and
// Navigation tabs peek instead (peekSidebar): no scrim at all, the client
// undimmed, so a theme can be judged as it is picked. Every check reads the
// drawn window: the app alone, then with the dialog over it.

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// sbCSSColor parses #rrggbb, #rrggbbaa and rgba(r,g,b,a) into the colour
// and its alpha.
func sbCSSColor(t *testing.T, s string) (color.NRGBA, float64) {
	t.Helper()
	s = strings.TrimSpace(s)
	if in, ok := strings.CutPrefix(s, "rgba("); ok {
		p := strings.Split(strings.TrimSuffix(in, ")"), ",")
		if len(p) != 4 {
			t.Fatalf("bad colour %q", s)
		}
		var v [4]float64
		for i, x := range p {
			f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				t.Fatalf("bad colour %q", s)
			}
			v[i] = f
		}
		return color.NRGBA{uint8(v[0]), uint8(v[1]), uint8(v[2]), 0xff}, v[3]
	}
	h := strings.TrimPrefix(s, "#")
	a := 1.0
	if len(h) == 8 {
		n, err := strconv.ParseUint(h[6:], 16, 8)
		if err != nil {
			t.Fatalf("bad colour %q", s)
		}
		a, h = float64(n)/255, h[:6]
	}
	c := parseHexColor(h)
	if c.A == 0 {
		t.Fatalf("bad colour %q", s)
	}
	return c, a
}

// sbSamples are the window's pixels outside the dialog and its shadow,
// below the top bar and above the composer (which blinks): the app the
// scrim covers. Every third pixel.
func sbSamples(sz image.Point, dlg image.Rectangle) []image.Point {
	const shadow = 72 // the card's box-shadow reaches 18+48 px below it
	keep := dlg.Inset(-shadow)
	var out []image.Point
	for y := theme.TopNavH + 8; y < sz.Y-230; y += 3 {
		for x := 0; x < sz.X; x += 3 {
			if p := image.Pt(x, y); !p.In(keep) {
				out = append(out, p)
			}
		}
	}
	return out
}

func sbLuma(c color.RGBA) float64 {
	return .299*float64(c.R) + .587*float64(c.G) + .114*float64(c.B)
}

// sbCorr is the correlation of the app's and the dimmed window's luma over
// pts (0 when either is flat).
func sbCorr(app, got *image.RGBA, pts []image.Point) (r, sdApp, sdGot float64) {
	var sa, sg, saa, sgg, sag float64
	for _, p := range pts {
		a, g := sbLuma(app.RGBAAt(p.X, p.Y)), sbLuma(got.RGBAAt(p.X, p.Y))
		sa, sg, saa, sgg, sag = sa+a, sg+g, saa+a*a, sgg+g*g, sag+a*g
	}
	n := float64(len(pts))
	va, vg := saa/n-(sa/n)*(sa/n), sgg/n-(sg/n)*(sg/n)
	sdApp, sdGot = math.Sqrt(math.Max(va, 0)), math.Sqrt(math.Max(vg, 0))
	if sdApp == 0 || sdGot == 0 {
		return 0, sdApp, sdGot
	}
	return (sag/n - (sa/n)*(sg/n)) / (sdApp * sdGot), sdApp, sdGot
}

// sbOver is a browser's sRGB blend of c at alpha a over b.
func sbOver(c color.NRGBA, a float64, b color.RGBA) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*a + float64(y)*(1-a))) }
	return color.NRGBA{m(c.R, b.R), m(c.G, b.G), m(c.B, b.B), 0xff}
}

func sbNRGBA(c color.RGBA) color.NRGBA { return color.NRGBA{c.R, c.G, c.B, 0xff} }

// checkDimmed asserts the app is drawn behind the dialog and dimmed by the
// scrim c at alpha a: the window follows the app's shapes, and the message
// pane (the surface the scrim's alpha is corrected for: Gio blends in
// linear light, a browser in sRGB, so one fill matches Slack on one
// backdrop) comes out as Slack's sRGB blend of the scrim over it.
func checkDimmed(t *testing.T, tag string, app, got *image.RGBA, pts []image.Point, pane image.Rectangle, c color.NRGBA, a float64) {
	t.Helper()
	r, sdApp, sdGot := sbCorr(app, got, pts)
	t.Logf("%s: correlation %.3f (sd %.2f, the app's %.2f)", tag, r, sdGot, sdApp)
	if sdApp < 2 {
		t.Fatalf("%s: the app alone is flat (sd %.2f): nothing to dim", tag, sdApp)
	}
	if r < 0.7 {
		flat := sbNRGBA(got.RGBAAt(pts[0].X, pts[0].Y))
		t.Errorf("%s: the app isn't drawn behind the dialog: the window doesn't follow it (correlation %.2f, sd %.2f vs the app's %.2f; first sample %s)",
			tag, r, sdGot, sdApp, svHex(flat))
	}
	// the pane's own colour, where it shows beside the dialog
	counts := map[color.RGBA][]image.Point{}
	n := 0
	for _, p := range pts {
		if p.In(pane) {
			k := app.RGBAAt(p.X, p.Y)
			counts[k] = append(counts[k], p)
			n++
		}
	}
	var k color.RGBA
	for x, ps := range counts {
		if len(ps) > len(counts[k]) {
			k = x
		}
	}
	if n == 0 || len(counts[k]) < n/2 {
		t.Fatalf("%s: no pane colour beside the dialog (%d samples)", tag, n)
	}
	want := sbOver(c, a, k)
	var rs, gs, bs []int
	for _, p := range counts[k] {
		x := got.RGBAAt(p.X, p.Y)
		rs, gs, bs = append(rs, int(x.R)), append(gs, int(x.G)), append(bs, int(x.B))
	}
	med := func(v []int) uint8 { sort.Ints(v); return uint8(v[len(v)/2]) }
	g := color.NRGBA{med(rs), med(gs), med(bs), 0xff}
	if !svNear(g, want, 2) {
		t.Errorf("%s: the pane (%s alone) is %s behind the dialog; Slack's %s at %.2f over it is %s",
			tag, svHex(sbNRGBA(k)), svHex(g), svHex(c), a, svHex(want))
	}
}

// checkUndimmed asserts the window outside the dialog is the app as drawn
// alone (Appearance and Navigation: no scrim).
func checkUndimmed(t *testing.T, tag string, app, got *image.RGBA, pts []image.Point) {
	t.Helper()
	same := 0
	var bad image.Point
	for _, p := range pts {
		if svNear(sbNRGBA(got.RGBAAt(p.X, p.Y)), sbNRGBA(app.RGBAAt(p.X, p.Y)), 2) {
			same++
		} else {
			bad = p
		}
	}
	if share := float64(same) / float64(len(pts)); share < 0.98 {
		t.Errorf("%s: the app is dimmed behind the dialog: %.1f%% of it is drawn as it is alone (e.g. %v: %s, alone %s)",
			tag, share*100, bad, svHex(sbNRGBA(got.RGBAAt(bad.X, bad.Y))), svHex(sbNRGBA(app.RGBAAt(bad.X, bad.Y))))
	}
}

func TestSlickViewPrefsBackdrop(t *testing.T) {
	e := svThemesEnv(t)
	sh := newSVShooter(t, e)
	p := newSlickPage(e.a)
	defer p.close()
	var pm *prefsModal
	prefsTap = func(x *prefsModal) { pm = x }
	t.Cleanup(func() { prefsTap = nil })

	// Slack's own --dt_color-base-modal (popups' measured Backdrop):
	// rgba(29,28,29,.7) light, rgba(0,0,0,.7) dark.
	slackLight, slackDark := "rgba(29,28,29,0.7)", "rgba(0,0,0,0.7)"
	verdant := svReadTheme(t, e.dir, "verdant")
	if got := fmt.Sprint(verdant.Vars["--dt_color-base-modal"]); got != "rgba(13,24,16,0.98)" {
		t.Errorf("upstream Verdant Circuit's scrim is %s now, the test expects rgba(13,24,16,0.98)", got)
	}
	nord := svReadTheme(t, e.dir, "nord")

	pick := func(id string, dark bool) {
		t.Helper()
		e.mode(dark)
		if err := e.s.SetTheme(""); err != nil {
			t.Fatal(err)
		}
		e.until(10*time.Second, "no theme", func() bool { return !e.s.ThemeApplies() })
		if id == "" {
			return
		}
		j := svReadTheme(t, e.dir, id)
		p.pickTheme(e.card(id))
		pane, _ := svPaneOf(j)
		e.until(15*time.Second, id+" applied", func() bool {
			return e.s.ThemeApplies() && e.a.th.Dark == (j.Mode == "dark") && e.a.th.C.Bg == pane
		})
	}
	open := func(section string) *popups.Modal {
		t.Helper()
		e.a.showPrefs(section)
		var m *popups.Modal
		e.until(10*time.Second, "Preferences", func() bool {
			x, ok := topOverlay[*popups.Modal](e.a)
			m = x
			return ok && x.Title() == "Preferences" && !x.Rect().Empty()
		})
		for i := 0; i < 8; i++ { // the Themes tab's cards and the fonts settle
			e.frame()
		}
		time.Sleep(250 * time.Millisecond) // past a scrim fade
		return m
	}
	closeAll := func() {
		for {
			m, ok := topOverlay[*popups.Modal](e.a)
			if !ok {
				break
			}
			m.Close()
			e.frame()
		}
		for i := 0; i < 8; i++ {
			e.frame()
		}
	}

	for _, c := range []struct {
		name, theme, scrim string
		dark               bool
	}{
		{"verdant", "verdant", fmt.Sprint(verdant.Vars["--dt_color-base-modal"]), true},
		{"nord", "nord", fmt.Sprint(nord.Vars["--dt_color-base-modal"]), true},
		{"slack-light", "", slackLight, false},
		{"slack-dark", "", slackDark, true},
	} {
		pick(c.theme, c.dark)
		closeAll()
		app := sh.shot()
		sh.save("backdrop-"+c.name+"-app", app)

		// Preferences > Themes (Slick's tab): the scrim
		m := open(themesTabID)
		got := sh.shot()
		sh.save("backdrop-"+c.name+"-themes", got)
		col, a := sbCSSColor(t, c.scrim)
		pts := sbSamples(e.sz, m.Rect())
		checkDimmed(t, c.name+" on Themes", app, got, pts, e.regions().pane, col, a)

		// Appearance (Slack's peekSidebar tab): no scrim, after its fade
		pm.selectTab("appearance")
		e.frame()
		time.Sleep(250 * time.Millisecond)
		got = sh.shot()
		sh.save("backdrop-"+c.name+"-appearance", got)
		checkUndimmed(t, c.name+" on Appearance", app, got, sbSamples(e.sz, m.Rect()))

		// back to a tab without peekSidebar (Notifications): the scrim fades back
		pm.selectTab("notifications")
		e.frame()
		time.Sleep(250 * time.Millisecond)
		got = sh.shot()
		sh.save("backdrop-"+c.name+"-notifications", got)
		checkDimmed(t, c.name+" on Notifications", app, got, sbSamples(e.sz, m.Rect()), e.regions().pane, col, a)
		closeAll()

		// opened on Navigation (the other peekSidebar tab): no scrim from the start
		m = open("navigation")
		got = sh.shot()
		sh.save("backdrop-"+c.name+"-navigation", got)
		checkUndimmed(t, c.name+" on Navigation", app, got, sbSamples(e.sz, m.Rect()))
		closeAll()
	}
}
