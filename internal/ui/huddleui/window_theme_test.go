package huddleui

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// karimTheme is Karim's account theme (α.112 on Windows: light, custom
// #FED4BE; theme_contrast_test.go's "karim picker light").
const karimTheme = `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`

// Karim, α.112: the huddle window is "not following color theme": its
// background was Slack's default aubergine and the mic caret's menu dark,
// under a light custom theme. Slack's huddle window is a client window in
// your theme (window_theme.go): the window shows the theme's
// .p-theme_background, the toolbar its chrome tokens, and the Audio
// settings menu is a c-menu of your mode.
func TestHuddleWindowFollowsYourTheme(t *testing.T) {
	const W, H = 589, 351
	for _, tc := range []struct {
		name string
		ia   string // "" = Slack's default theme
		dark bool
	}{
		{"karim-light", karimTheme, false},
		{"default-dark", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newShotEnv(t, tc.dark)
			main := e.h.th
			if tc.ia != "" {
				ia, ok := theme.ParseIATheme(tc.ia)
				if !ok {
					t.Fatal("bad theme JSON")
				}
				main.Restyle = func(th *theme.Theme, p *theme.Palette) { theme.ApplyIATheme(p, ia, th.Dark, true) }
				main.Refresh()
			}
			s := e.join(nil, nil)
			waitState(t, s, "connected", func(st huddle.State) bool { return st.Phase == huddle.Connected })
			// the main window's frame: it draws the docked bar in your theme
			var ops op.Ops
			e.c.LayoutDock(shotCtx(&ops, 1000, dockBar+dockPad), main)
			for i := 0; i < 2; i++ {
				ops.Reset()
				e.w.layout(shotCtx(&ops, W, H))
			}
			img := render(t, "theme-window-"+tc.name, W, H, func(gtx C) { e.w.layout(gtx) })

			// the window's background, at the title bar's left end: the
			// theme's window colours (the gradient runs between them)
			bg := img.RGBAAt(8*shotScale, 16*shotScale)
			got := color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: 0xff}
			if d := segDist(got, main.C.ChromeA, main.C.ChromeB); d > 14 {
				t.Errorf("the window's background is %v, not your theme's window colours (%v to %v; %d off; Slack's default aubergine is %v)",
					got, main.C.ChromeA, main.C.ChromeB, d, hudFrame)
			}

			// the mic caret's Audio settings menu: a c-menu in your mode
			e.w.toggleAudio(s)
			if e.w.audio == nil {
				t.Fatal("no Audio settings menu")
			}
			img = render(t, "theme-window-"+tc.name+"-audio-menu", W, H, func(gtx C) { e.w.layout(gtx) })
			r := e.w.audio.Rect()
			in := image.Rect(r.Min.X+r.Dx()/4, r.Min.Y+r.Dy()/4, r.Max.X-r.Dx()/4, r.Max.Y-r.Dy()/4)
			if in.Empty() {
				t.Fatalf("the menu's rectangle %v", r)
			}
			if n := pixeltest.Count(img, in, main.C.BgRaised, 8); n < in.Dx()*in.Dy()/2 {
				t.Errorf("the Audio settings menu isn't your mode's menu: %d of %d px are %v (dark=%v)", n, in.Dx()*in.Dy(), main.C.BgRaised, tc.dark)
			}
			e.w.closeMenus()
		})
	}
}

// segDist is how far c is from the line between a and b (per channel, the
// largest), for a colour of a gradient between them.
func segDist(c, a, b color.NRGBA) int {
	best := 1 << 30
	for i := 0; i <= 64; i++ {
		t := float64(i) / 64
		lerp := func(x, y uint8) int { return int(float64(x) + (float64(y)-float64(x))*t + .5) }
		d := 0
		for _, p := range [][2]int{{int(c.R), lerp(a.R, b.R)}, {int(c.G), lerp(a.G, b.G)}, {int(c.B), lerp(a.B, b.B)}} {
			if v := p[0] - p[1]; v > d {
				d = v
			} else if -v > d {
				d = -v
			}
		}
		best = min(best, d)
	}
	return best
}
