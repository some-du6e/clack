package huddleui

import (
	"image"
	"image/color"
	"math"
	"testing"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// karimTheme is Karim's account theme (α.112 on Windows: light, custom
// #FED4BE; theme_contrast_test.go's "karim picker light").
const karimTheme = `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`

// Themes whose chrome scope isn't the sidebar's (theme_contrast_test.go's
// cases, Slack's own scopes in internal/ui/testdata/theme-oracle.json):
// Ochin (a light preset at brightness 5, Darker sidebars off: the chrome
// inverted over its dark blue window, the sidebar light), Slack's default
// Aubergine with Darker sidebars unticked, and Karim's theme as Slack's
// Import stores it (brightness 6: the inverted chrome's white over a pale
// window).
const (
	ochinTheme        = `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`
	aubergineNoDarker = `{"primary":{"palette":"aubergine"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":6,"sidebarInverted":false}`
	karimImportTheme  = `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`
)

// themed is a theme in mode dark with account theme ia ("": Slack's
// default), as the main window builds it.
func themed(t *testing.T, ia string, dark bool) *theme.Theme {
	t.Helper()
	th := theme.New(dark)
	if ia != "" {
		iat, ok := theme.ParseIATheme(ia)
		if !ok {
			t.Fatal("bad theme JSON")
		}
		th.Restyle = func(th *theme.Theme, p *theme.Palette) { theme.ApplyIATheme(p, iat, th.Dark, true) }
		th.Refresh()
	}
	return th
}

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
		{"ochin-light", ochinTheme, false},
		{"aubergine-darker-sidebars-off", aubergineNoDarker, false},
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

			// the toolbar's icons (the footer's left half: the mic, camera
			// and share buttons; Leave is at the right end) in the chrome's
			// ink, the title's: never the sidebar's, dark on Ochin's dark
			// blue window (exact pixels: an unmuted mic's base-pry tile is
			// near the chrome's white there)
			foot := image.Rect(0, (H-winFooterH)*shotScale, W/2*shotScale, H*shotScale)
			if n := pixeltest.Count(img, foot, main.C.ChromeText, 8); n < 40 {
				t.Errorf("the toolbar's icons aren't in your theme's chrome ink %v: %d px of it in the toolbar's left half", main.C.ChromeText, n)
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

// Slack's HuddleToolbar paints in the chrome's scope (boot-shared
// 13b0d600: HuddleToolbar puts useInvertedTheme({view: "chrome"})'s class
// on its toolbarContainer; vYdp inverts "chrome" in a light client at a
// dark brightness, "sidebar" only with Darker sidebars on as well): its
// tile surf-inv-pry, :hover surf-inv-ter, the segment's line otl-inv-pry,
// the icons content-inv-pry, a toggled button base-pry with content-ter.
// These are the chrome scope's tokens in Slack's own code
// (theme-oracle.json), and over a dark window they read.
func TestHuddleToolbarInksLikeSlack(t *testing.T) {
	type inks struct{ icon, btn, btnHov, divider, btnOn, onFg color.NRGBA }
	for _, tc := range []struct {
		name, ia string
		dark     bool
		want     inks
		reads    bool // the window is dark: Slack's ink reads over it
	}{
		{"ochin light", ochinTheme, false, inks{rgbHex(0xffffff), rgbaHex(0xe6f5fe, 0x40), rgbaHex(0xe6f5fe, 0x14), rgbaHex(0xe6f5fe, 0x38), rgbHex(0xe6f5fe), rgbHex(0x122a59)}, true},
		{"aubergine, Darker sidebars off", aubergineNoDarker, false, inks{rgbHex(0xffffff), rgbaHex(0xf9edff, 0x40), rgbaHex(0xf9edff, 0x14), rgbaHex(0xf9edff, 0x38), rgbHex(0xf9edff), rgbHex(0x39063a)}, true},
		{"karim picker light", karimTheme, false, inks{rgbHex(0x1c0901), rgbaHex(0x1c0901, 0x21), rgbaHex(0x41281a, 0x21), rgbaHex(0x41281a, 0x21), rgbHex(0x573c2c), rgbHex(0xfff4f0)}, false},
		{"default dark", "", true, inks{rgbHex(0xf8f8f8), rgbaHex(0xefe1f5, 0x40), rgbaHex(0xefe1f5, 0x14), rgbaHex(0xefe1f5, 0x38), rgbHex(0xe3ceeb), rgbHex(0x200221)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := windowInks(themed(t, tc.ia, tc.dark))
			if got := (inks{k.icon, k.btn, k.btnHov, k.divider, k.btnOn, k.onFg}); got != tc.want {
				t.Errorf("the toolbar's inks (icon, tile, hover, divider, toggled, toggled ink) are %v, Slack's chrome scope %v", got, tc.want)
			}
			if k.title != tc.want.icon {
				t.Errorf("the title is %v, Slack's %v", k.title, tc.want.icon)
			}
			if tc.reads {
				for _, bg := range []color.NRGBA{k.bgA, k.bgB} {
					if c := contrastRatio(k.icon, bg); c < 4.5 {
						t.Errorf("the toolbar's icons %v over the window's %v: %.2f:1", k.icon, bg, c)
					}
				}
			}
		})
	}
}

// The Windows caption takes the window's colour, and its title and its
// buttons' glyphs are both chosen by that colour's lightness: an inverted
// chrome's white title over a pale window (Karim's theme as Slack's Import
// stores it) would vanish into a light caption.
func TestHuddleWindowCaptionReads(t *testing.T) {
	for _, tc := range []struct {
		name, ia string
		dark     bool
	}{
		{"karim import light", karimImportTheme, false},
		{"karim picker light", karimTheme, false},
		{"ochin light", ochinTheme, false},
		{"default light", "", false},
		{"default dark", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl := windowInks(themed(t, tc.ia, tc.dark)).caption()
			if c := contrastRatio(cl.text, cl.bg); c < 4.5 {
				t.Errorf("the caption's title %v on %v: %.2f:1", cl.text, cl.bg, c)
			}
			if light := luma(cl.text) >= 0.5; light != cl.dark {
				t.Errorf("a caption with dark=%v (its buttons' glyphs) has a title %v", cl.dark, cl.text)
			}
		})
	}
}

func rgbHex(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func rgbaHex(v uint32, a uint8) color.NRGBA { c := rgbHex(v); c.A = a; return c }

// contrastRatio is WCAG's contrast between two opaque colours.
func contrastRatio(a, b color.NRGBA) float64 {
	lum := func(c color.NRGBA) float64 {
		ch := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
